# CyberStack 0.4 — Pull Pipeline Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drive cyberstackd's cold-pull P50 from 860ms (v0.3.0) down to ≤520ms (within 50ms of OrbStack's measured 467ms baseline) on `mirror.gcr.io/library/alpine:latest`, n=100, NordVPN off.

**Architecture:** Two independent halves bench-able on their own. (a) A `go-containerregistry` adapter implementing `types.ImageSource` so `copy.Image` uses g-c-r's `remote.Image` for the source-side fetch — gets us a shared `*http.Transport` with `ForceAttemptHTTP2: true` and `MaxIdleConnsPerHost: 50`, attacking the 571ms pre-phase round-trip cost. (b) A `go.mod replace` against `containers/storage` that patches the overlay graph driver's `var untar = chrootarchive.UntarUncompressed` to `var untar = archive.UntarUncompressed`, eliminating the per-pull `reexec` fork inside the 343ms blob-copy phase. Destination side stays `is.Transport.NewImageDestination()` writing into `containers/storage`.

**Tech Stack:** Go 1.24, `github.com/google/go-containerregistry` v0.20.3 (already a transitive dep), `containers/image` v5.36.2 (unchanged), `containers/storage` v1.59.1 (forked via `go.mod replace` to a vendored copy), zig+CGO cross-compile to `linux/aarch64-musl` (unchanged).

---

## Spec reference

This plan implements `docs/superpowers/specs/2026-05-01-cyberstack-0.4-pull-pipeline-design.md` in full. Read it first — it contains the 0.3 phase-tracer evidence (571ms pre-phase, 343ms blob-copy, 98% idle CPU) that motivates each design decision.

The 0.3.0 baseline `cyber-stack/benchmarks/cyber-0.3.0.json` (P50 860ms) and OrbStack baseline `cyber-stack/benchmarks/orb-vpn-off.json` (P50 467ms) are the comparison anchors. New runs land in `cyber-stack/benchmarks/` with labels `cyber-0.4-<phase>-vpn-off`.

---

## File structure

| Path | Status | Responsibility |
|---|---|---|
| `cyber-stack/internal/agent/imagestore/gcrsource.go` | **new** | `gcrImageReference` + `gcrImageSource` implementing `types.ImageReference` + `types.ImageSource`. Wraps `go-containerregistry`'s `remote.Image` for fetch. ~300 lines. |
| `cyber-stack/internal/agent/imagestore/gcrsource_test.go` | **new** | Compile-time interface assertion; integration test against `mirror.gcr.io/library/alpine` (skipped on no-network). |
| `cyber-stack/internal/agent/imagestore/transport.go` | **new** | Process-singleton `*http.Transport` constructor with `ForceAttemptHTTP2: true`, `MaxIdleConnsPerHost: 50`, `IdleConnTimeout: 90s`. Used by the gcr adapter, available to other agent code that needs HTTPS later. |
| `cyber-stack/internal/agent/imagestore/store.go` | modify | `Pull` switches between the legacy `docker.ParseReference` source and the new gcr source via `CYBERSTACK_PULL_SOURCE=gcr` env var. After Phase A bench passes, gcr becomes default and the env var is removed. |
| `cyber-stack/go.mod` | modify | Add `github.com/google/go-containerregistry` as a direct dep (currently transitive). Add `replace github.com/containers/storage => ./third_party/containers-storage` for Phase B. |
| `cyber-stack/third_party/containers-storage/` | **new** | Vendored copy of `github.com/containers/storage@v1.59.1` with one-line patch to `drivers/overlay/overlay.go`. README documents the patch + how to rebase on upstream version bumps. |
| `cyber-stack/third_party/containers-storage/README.md` | **new** | Why we forked, what changed (one line), security trade-off, how to forward-port. |
| `cyber-stack/docs/benchmarking.md` | modify | Add 0.4.0 results section after the 0.3.0 one. Update the comparison table. |
| `cyber-stack/internal/httpapi/version.go` | modify | Bump `CyberStackVersion` to `0.4.0`. |

The two new source files in `imagestore/` keep the package focused (one file per concept: pull source adapter, transport singleton). `gcrsource.go` is the only file that touches `go-containerregistry`.

---

## Phase A: go-containerregistry pull source adapter

Six tasks, ~1–2 days. Each smoke-testable on its own. Lands behind a feature flag; flag removed in Task 7 once benchmark validates.

### Task 1: Add go-containerregistry direct dependency + transport singleton

**Files:**
- Modify: `cyber-stack/go.mod`
- Create: `cyber-stack/internal/agent/imagestore/transport.go`
- Create: `cyber-stack/internal/agent/imagestore/transport_test.go`

- [ ] **Step 1: Promote g-c-r to a direct dependency**

g-c-r is currently a transitive dep. Promote it so we can import the `remote` package directly:

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
go get github.com/google/go-containerregistry@v0.20.3
go mod tidy
```

Expected: `go.mod` now lists `github.com/google/go-containerregistry v0.20.3` in the direct-deps block (no `// indirect` comment).

- [ ] **Step 2: Write the failing test**

Create `cyber-stack/internal/agent/imagestore/transport_test.go`:

```go
//go:build linux

package imagestore

import (
	"net/http"
	"testing"
)

func TestSharedTransport_HasH2AndPooling(t *testing.T) {
	tr := SharedTransport()
	httpTr, ok := tr.(*http.Transport)
	if !ok {
		t.Fatalf("SharedTransport must return *http.Transport for our config asserts, got %T", tr)
	}
	if !httpTr.ForceAttemptHTTP2 {
		t.Errorf("ForceAttemptHTTP2 must be true so ALPN negotiates h2 even when callers don't set NextProtos")
	}
	if got := httpTr.MaxIdleConnsPerHost; got != 50 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 50 (stdlib default of 2 throttles registry pulls)", got)
	}
	if got := httpTr.IdleConnTimeout; got.Seconds() != 90 {
		t.Errorf("IdleConnTimeout = %v, want 90s (matches g-c-r's DefaultTransport)", got)
	}
}

func TestSharedTransport_ReturnsSameInstance(t *testing.T) {
	a := SharedTransport()
	b := SharedTransport()
	if a != b {
		t.Errorf("SharedTransport must be a process singleton; got two different instances %p vs %p", a, b)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
go test ./internal/agent/imagestore/... -run TestSharedTransport
```

Expected: FAIL — `undefined: SharedTransport`.

- [ ] **Step 4: Implement `SharedTransport`**

Create `cyber-stack/internal/agent/imagestore/transport.go`:

```go
//go:build linux

package imagestore

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// SharedTransport returns a process-singleton *http.Transport configured for
// registry pulls. We own the transport (not containers/image) so connection
// pooling, HTTP/2 negotiation, and TLS-handshake amortisation work across
// pulls. Mirrors go-containerregistry's DefaultTransport (which we benched
// against during 0.4 design) plus an explicit ForceAttemptHTTP2.
//
// Singleton: callers must not mutate the returned transport; treat it as
// read-only after construction. Tests may compare instances by pointer.
func SharedTransport() http.RoundTripper {
	sharedTransportOnce.Do(func() {
		sharedTransport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   50,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	})
	return sharedTransport
}

var (
	sharedTransport     *http.Transport
	sharedTransportOnce sync.Once
)
```

- [ ] **Step 5: Run the test, expect PASS**

```bash
go test ./internal/agent/imagestore/... -run TestSharedTransport -v
```

Expected: PASS — both subtests green.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/agent/imagestore/transport.go internal/agent/imagestore/transport_test.go
git commit -m "agent: add SharedTransport singleton + promote g-c-r to direct dep

Process-wide *http.Transport with ForceAttemptHTTP2 + MaxIdleConnsPerHost=50.
Foundation for the g-c-r-backed pull source landing next; also available to
other agent code that needs an HTTP client into a registry."
```

### Task 2: Skeleton gcrImageReference + gcrImageSource with compile-time interface assertions

**Files:**
- Create: `cyber-stack/internal/agent/imagestore/gcrsource.go`
- Create: `cyber-stack/internal/agent/imagestore/gcrsource_test.go`

- [ ] **Step 1: Write the failing test (compile-time interface assertion)**

Create `cyber-stack/internal/agent/imagestore/gcrsource_test.go`:

```go
//go:build linux

package imagestore

import (
	"testing"

	"github.com/containers/image/v5/types"
)

// Compile-time assertions: gcrImageReference and gcrImageSource MUST satisfy
// the public containers/image interfaces. If this file fails to build, the
// adapter is incomplete and copy.Image won't accept it as a source.
var (
	_ types.ImageReference = (*gcrImageReference)(nil)
	_ types.ImageSource    = (*gcrImageSource)(nil)
)

func TestGcrImageReference_ParseValidReference(t *testing.T) {
	ref, err := newGcrImageReference("mirror.gcr.io/library/alpine:latest")
	if err != nil {
		t.Fatalf("newGcrImageReference returned error on a valid reference: %v", err)
	}
	if ref == nil {
		t.Fatal("newGcrImageReference returned nil ref with no error")
	}
	if ref.StringWithinTransport() != "//mirror.gcr.io/library/alpine:latest" {
		t.Errorf("StringWithinTransport = %q, want %q",
			ref.StringWithinTransport(), "//mirror.gcr.io/library/alpine:latest")
	}
}

func TestGcrImageReference_RejectsInvalid(t *testing.T) {
	if _, err := newGcrImageReference(""); err == nil {
		t.Error("empty ref must error")
	}
	if _, err := newGcrImageReference("@@@bogus@@@"); err == nil {
		t.Error("malformed ref must error")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/agent/imagestore/... -run TestGcrImageReference
```

Expected: FAIL with `undefined: gcrImageReference`, `undefined: gcrImageSource`, `undefined: newGcrImageReference`.

- [ ] **Step 3: Write the skeleton**

Create `cyber-stack/internal/agent/imagestore/gcrsource.go`:

```go
//go:build linux

package imagestore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/image/v5/types"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/opencontainers/go-digest"
)

// gcrImageReference is a types.ImageReference backed by the
// go-containerregistry name parser. It exists only so we can hand a source
// to copy.Image that uses g-c-r's transport-injectable pull pipeline.
//
// The destination side stays containers/image's is.Transport (writing into
// containers/storage). copy.Image is the orchestrator; we only swap the
// source.
type gcrImageReference struct {
	raw      string         // input string ("mirror.gcr.io/library/alpine:latest")
	gcrRef   name.Reference // parsed by g-c-r
	dockerRef reference.Named // parsed by containers/image (for policy-config compatibility)
}

func newGcrImageReference(raw string) (*gcrImageReference, error) {
	if raw == "" {
		return nil, errors.New("empty image reference")
	}
	gcrRef, err := name.ParseReference(raw)
	if err != nil {
		return nil, fmt.Errorf("parse gcr ref %q: %w", raw, err)
	}
	dockerRef, err := reference.ParseNormalizedNamed(raw)
	if err != nil {
		return nil, fmt.Errorf("parse docker ref %q: %w", raw, err)
	}
	return &gcrImageReference{raw: raw, gcrRef: gcrRef, dockerRef: dockerRef}, nil
}

// types.ImageReference implementation -----------------------------------

func (r *gcrImageReference) Transport() types.ImageTransport { return gcrTransport{} }
func (r *gcrImageReference) StringWithinTransport() string   { return "//" + r.raw }
func (r *gcrImageReference) DockerReference() reference.Named {
	return r.dockerRef
}
func (r *gcrImageReference) PolicyConfigurationIdentity() string  { return r.raw }
func (r *gcrImageReference) PolicyConfigurationNamespaces() []string {
	return nil
}

func (r *gcrImageReference) NewImage(ctx context.Context, sys *types.SystemContext) (types.ImageCloser, error) {
	return nil, errors.New("gcrImageReference.NewImage not implemented; use NewImageSource via copy.Image")
}

func (r *gcrImageReference) NewImageDestination(ctx context.Context, sys *types.SystemContext) (types.ImageDestination, error) {
	return nil, errors.New("gcrImageReference is source-only; destinations come from is.Transport")
}

func (r *gcrImageReference) DeleteImage(ctx context.Context, sys *types.SystemContext) error {
	return errors.New("gcrImageReference is source-only; deletion is via containers/storage")
}

func (r *gcrImageReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	return nil, errors.New("not implemented yet")
}

// gcrTransport is a no-op types.ImageTransport. We don't register it with
// containers/image's transports.KnownTransports — copy.Image takes the
// reference directly.
type gcrTransport struct{}

func (gcrTransport) Name() string { return "gcr" }

func (gcrTransport) ParseReference(s string) (types.ImageReference, error) {
	return newGcrImageReference(s)
}

func (gcrTransport) ValidatePolicyConfigurationScope(s string) error { return nil }

// gcrImageSource is the source-side adapter. Methods land in Tasks 3–5.
type gcrImageSource struct {
	ref     *gcrImageReference
	gcrImg  remote.Descriptor // populated in NewImageSource
	closed  bool
}

// types.ImageSource implementation (stubs; filled in by Tasks 3–5) -----

func (s *gcrImageSource) Reference() types.ImageReference { return s.ref }
func (s *gcrImageSource) Close() error {
	s.closed = true
	return nil
}

func (s *gcrImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	return nil, "", errors.New("not implemented yet")
}

func (s *gcrImageSource) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	return nil, 0, errors.New("not implemented yet")
}

func (s *gcrImageSource) HasThreadSafeGetBlob() bool { return true }

func (s *gcrImageSource) GetSignatures(ctx context.Context, instanceDigest *digest.Digest) ([][]byte, error) {
	// We don't fetch registry signatures — our policy is insecureAcceptAnything.
	return nil, nil
}

func (s *gcrImageSource) LayerInfosForCopy(ctx context.Context, instanceDigest *digest.Digest) ([]types.BlobInfo, error) {
	// nil = "values in the manifest are fine, use them as-is".
	return nil, nil
}
```

- [ ] **Step 4: Run the test, expect PASS**

```bash
go test ./internal/agent/imagestore/... -run TestGcrImageReference -v
```

Expected: PASS — both interface assertions compile, `TestGcrImageReference_ParseValidReference` and `TestGcrImageReference_RejectsInvalid` both green.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/imagestore/gcrsource.go internal/agent/imagestore/gcrsource_test.go
git commit -m "agent: skeleton gcrImageReference + gcrImageSource

Compile-time interface assertions confirm the adapter satisfies
types.ImageReference + types.ImageSource. NewImageSource and the data-fetching
methods (GetManifest, GetBlob) are stubs; tasks 3-5 implement them."
```

### Task 3: Implement `NewImageSource` + `GetManifest`

**Files:**
- Modify: `cyber-stack/internal/agent/imagestore/gcrsource.go`
- Modify: `cyber-stack/internal/agent/imagestore/gcrsource_test.go`

- [ ] **Step 1: Write the failing integration test**

Append to `gcrsource_test.go`:

```go
func TestGcrImageSource_GetManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: pulls from mirror.gcr.io")
	}
	ref, err := newGcrImageReference("mirror.gcr.io/library/alpine:latest")
	if err != nil {
		t.Fatalf("newGcrImageReference: %v", err)
	}
	src, err := ref.NewImageSource(context.Background(), nil)
	if err != nil {
		t.Fatalf("NewImageSource: %v", err)
	}
	defer src.Close()
	manifest, mediaType, err := src.GetManifest(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if len(manifest) == 0 {
		t.Error("manifest is empty")
	}
	if mediaType == "" {
		t.Error("mediaType is empty — copy.Image requires it to dispatch on schema")
	}
	t.Logf("manifest %d bytes, mediaType=%s", len(manifest), mediaType)
}
```

Add the import: `"context"` at the top of the test file if not already present.

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/agent/imagestore/... -run TestGcrImageSource_GetManifest -v
```

Expected: FAIL with `not implemented yet` (from the stub).

- [ ] **Step 3: Implement `NewImageSource` + `GetManifest`**

In `gcrsource.go`, replace `NewImageSource` and `GetManifest`:

```go
func (r *gcrImageReference) NewImageSource(ctx context.Context, sys *types.SystemContext) (types.ImageSource, error) {
	desc, err := remote.Get(r.gcrRef,
		remote.WithContext(ctx),
		remote.WithTransport(SharedTransport()),
	)
	if err != nil {
		return nil, fmt.Errorf("remote.Get %s: %w", r.raw, err)
	}
	return &gcrImageSource{ref: r, gcrImg: *desc}, nil
}

func (s *gcrImageSource) GetManifest(ctx context.Context, instanceDigest *digest.Digest) ([]byte, string, error) {
	if instanceDigest != nil {
		// Manifest-list child: fetch via remote.Get on the digested ref.
		digested := s.ref.gcrRef.Context().Digest(instanceDigest.String())
		desc, err := remote.Get(digested,
			remote.WithContext(ctx),
			remote.WithTransport(SharedTransport()),
		)
		if err != nil {
			return nil, "", fmt.Errorf("remote.Get child %s: %w", instanceDigest, err)
		}
		return desc.Manifest, string(desc.MediaType), nil
	}
	return s.gcrImg.Manifest, string(s.gcrImg.MediaType), nil
}
```

Note: `remote.Get` returns the manifest bytes + content-type in one round trip — no separate manifest fetch later.

- [ ] **Step 4: Run the test, expect PASS**

```bash
go test ./internal/agent/imagestore/... -run TestGcrImageSource_GetManifest -v
```

Expected: PASS, log line shows manifest size > 0 and mediaType is one of `application/vnd.docker.distribution.manifest.v2+json`, `application/vnd.docker.distribution.manifest.list.v2+json`, or `application/vnd.oci.image.manifest.v1+json`.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/imagestore/gcrsource.go internal/agent/imagestore/gcrsource_test.go
git commit -m "agent: gcrImageSource.NewImageSource + GetManifest via remote.Get

remote.Get returns manifest bytes + media type in a single round trip, so
copy.Image's manifest fetch is a cache hit (no extra request). Uses
SharedTransport so subsequent pulls reuse the connection pool."
```

### Task 4: Implement `GetBlob`

**Files:**
- Modify: `cyber-stack/internal/agent/imagestore/gcrsource.go`
- Modify: `cyber-stack/internal/agent/imagestore/gcrsource_test.go`

- [ ] **Step 1: Write the failing test**

Append to `gcrsource_test.go`:

```go
func TestGcrImageSource_GetBlob(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: pulls from mirror.gcr.io")
	}
	ref, err := newGcrImageReference("mirror.gcr.io/library/alpine:latest")
	if err != nil {
		t.Fatalf("newGcrImageReference: %v", err)
	}
	src, err := ref.NewImageSource(context.Background(), nil)
	if err != nil {
		t.Fatalf("NewImageSource: %v", err)
	}
	defer src.Close()

	// Fetch the manifest, parse out the config blob digest, then GetBlob it.
	manBytes, _, err := src.GetManifest(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	// alpine's manifest is small; brute-force scan for the config digest.
	cfgDigest := scanFirstDigest(t, manBytes, "config")
	blob, size, err := src.GetBlob(
		context.Background(),
		types.BlobInfo{Digest: digest.Digest(cfgDigest)},
		nil,
	)
	if err != nil {
		t.Fatalf("GetBlob config: %v", err)
	}
	defer blob.Close()
	body, err := io.ReadAll(blob)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if int64(len(body)) != size && size != -1 {
		t.Errorf("read %d bytes, GetBlob reported size %d", len(body), size)
	}
	if !bytes.Contains(body, []byte(`"architecture"`)) {
		t.Errorf("config blob does not look like a v1 image config (no \"architecture\" field): %s", body)
	}
}

// scanFirstDigest pulls the first sha256:... digest from a manifest near a
// keyword. Avoids pulling in a JSON parser for a one-off integration test.
func scanFirstDigest(t *testing.T, manifest []byte, near string) string {
	t.Helper()
	idx := bytes.Index(manifest, []byte(near))
	if idx == -1 {
		t.Fatalf("manifest does not contain %q: %s", near, manifest)
	}
	rest := manifest[idx:]
	d := bytes.Index(rest, []byte("sha256:"))
	if d == -1 {
		t.Fatalf("no sha256: digest after %q: %s", near, rest)
	}
	end := d
	for end < len(rest) && (rest[end] != '"' && rest[end] != ',' && rest[end] != ' ' && rest[end] != '\n') {
		end++
	}
	return string(rest[d:end])
}
```

Add imports if not present: `"bytes"`, `"io"`, `"github.com/opencontainers/go-digest"`.

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/agent/imagestore/... -run TestGcrImageSource_GetBlob -v
```

Expected: FAIL with `not implemented yet`.

- [ ] **Step 3: Implement `GetBlob`**

In `gcrsource.go`, replace `GetBlob`:

```go
func (s *gcrImageSource) GetBlob(ctx context.Context, info types.BlobInfo, cache types.BlobInfoCache) (io.ReadCloser, int64, error) {
	if info.Digest == "" {
		return nil, 0, errors.New("GetBlob requires a digest")
	}
	digested := s.ref.gcrRef.Context().Digest(info.Digest.String())
	layer, err := remote.Layer(digested,
		remote.WithContext(ctx),
		remote.WithTransport(SharedTransport()),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("remote.Layer %s: %w", info.Digest, err)
	}
	rc, err := layer.Compressed()
	if err != nil {
		return nil, 0, fmt.Errorf("layer.Compressed: %w", err)
	}
	size, sizeErr := layer.Size()
	if sizeErr != nil {
		size = -1
	}
	return rc, size, nil
}
```

`remote.Layer` works for both image config blobs and layer blobs (the registry exposes both via the same `/blobs/<digest>` endpoint).

- [ ] **Step 4: Run the test, expect PASS**

```bash
go test ./internal/agent/imagestore/... -run TestGcrImageSource_GetBlob -v
```

Expected: PASS, blob body contains `"architecture"`, size matches body length.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/imagestore/gcrsource.go internal/agent/imagestore/gcrsource_test.go
git commit -m "agent: gcrImageSource.GetBlob via remote.Layer

remote.Layer + Compressed() gives copy.Image the gzipped tar stream it
expects. Used for both config blobs and layer blobs (registry endpoint is
identical)."
```

### Task 5: Wire `CYBERSTACK_PULL_SOURCE=gcr` flag in Pull, smoke against running daemon

**Files:**
- Modify: `cyber-stack/internal/agent/imagestore/store.go`
- Modify: `cyber-stack/internal/agent/imagestore/gcrsource_test.go`

- [ ] **Step 1: Write the failing test**

Append to `gcrsource_test.go`:

```go
func TestGcrSourceFactoryReturnsCorrectType(t *testing.T) {
	src, err := newPullSource("gcr", "mirror.gcr.io/library/alpine:latest")
	if err != nil {
		t.Fatalf("newPullSource(gcr): %v", err)
	}
	if _, ok := src.(*gcrImageReference); !ok {
		t.Errorf("CYBERSTACK_PULL_SOURCE=gcr must return *gcrImageReference, got %T", src)
	}

	src2, err := newPullSource("docker", "mirror.gcr.io/library/alpine:latest")
	if err != nil {
		t.Fatalf("newPullSource(docker): %v", err)
	}
	// docker.ParseReference returns an unexported docker.dockerReference; we
	// just check it's NOT our gcr type.
	if _, ok := src2.(*gcrImageReference); ok {
		t.Error("CYBERSTACK_PULL_SOURCE=docker must NOT return *gcrImageReference")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/agent/imagestore/... -run TestGcrSourceFactoryReturnsCorrectType -v
```

Expected: FAIL with `undefined: newPullSource`.

- [ ] **Step 3: Implement the factory + wire it into `Pull`**

In `store.go`, change the `Pull` function to use the factory and add `newPullSource`:

```go
import (
	// ... existing imports ...
	"os"
)

// newPullSource picks the source-side ImageReference for a pull. "gcr"
// returns the go-containerregistry-backed adapter (HTTP/2, shared transport,
// connection pooling). Anything else falls through to containers/image's
// docker.ParseReference (the 0.3 default, kept while the gcr path is
// validated). Selected by CYBERSTACK_PULL_SOURCE env var; defaults to "gcr"
// after Phase A bench passes (Task 7).
func newPullSource(kind, ref string) (types.ImageReference, error) {
	switch kind {
	case "gcr":
		return newGcrImageReference(ref)
	default:
		return docker.ParseReference("//" + ref)
	}
}

func (s *Store) Pull(ctx context.Context, ref string) (*PullResult, error) {
	srcKind := os.Getenv("CYBERSTACK_PULL_SOURCE")
	if srcKind == "" {
		srcKind = "docker"
	}
	src, err := newPullSource(srcKind, ref)
	if err != nil {
		return nil, fmt.Errorf("parse src ref (%s): %w", srcKind, err)
	}
	dst, err := is.Transport.ParseStoreReference(s.cs, ref)
	if err != nil {
		return nil, fmt.Errorf("parse dst ref: %w", err)
	}

	policy, _ := signature.NewPolicyFromBytes([]byte(`{"default":[{"type":"insecureAcceptAnything"}]}`))
	polCtx, _ := signature.NewPolicyContext(policy)
	defer polCtx.Destroy()

	_, err = copy.Image(ctx, polCtx, dst, src, &copy.Options{
		SourceCtx: &types.SystemContext{},
	})
	if err != nil {
		return nil, fmt.Errorf("copy image: %w", err)
	}

	img, err := s.cs.Image(ref)
	if err != nil {
		return nil, fmt.Errorf("lookup pulled image: %w", err)
	}

	size, _ := s.cs.ImageSize(img.ID)
	return &PullResult{ImageID: img.ID, RepoTag: ref, Size: size}, nil
}
```

- [ ] **Step 4: Run the unit test, expect PASS**

```bash
go test ./internal/agent/imagestore/... -run TestGcrSourceFactoryReturnsCorrectType -v
```

Expected: PASS.

- [ ] **Step 5: Run the full unit test suite to confirm no regressions**

```bash
go test ./internal/...
```

Expected: all green.

- [ ] **Step 6: Cross-compile + boot disk**

```bash
make boot-disk
```

Expected: ends with `boot disk built: cyberstack-boot-arm64.img (256M)`.

- [ ] **Step 7: Smoke pull alpine via gcr path**

The agent inherits env from init.sh, so we need to set `CYBERSTACK_CMDLINE_PULL_SOURCE=gcr` somewhere it'll reach the agent. Simplest: add a temporary line to `guest/init.sh`. **This is reverted in Task 7.**

Edit `guest/init.sh` and find the agent launch line (`env GODEBUG=asyncpreemptoff=1 ...`), then change it to:

```sh
env GODEBUG=asyncpreemptoff=1 CYBERSTACK_UPLINK="$UPLINK" \
    CYBERSTACK_PULL_SOURCE=gcr \
    /usr/bin/cyberstack-agent
```

Rebuild + restart daemon (Ctrl-C the running daemon, restart):

```bash
make boot-disk
~/dev/cyber5-io/cyber-stack/bin/cyberstackd
```

In another terminal, smoke pull:

```bash
curl --unix-socket ~/.cyberstack/cyberstack.sock -X POST \
  "http://_/v1.43/images/create?fromImage=mirror.gcr.io/library/alpine&tag=latest"
```

Expected: `{"status":"Downloaded newer image for mirror.gcr.io/library/alpine:latest"}`.

If the pull errors, the gcr adapter is missing something. Check the daemon's stderr (run with `-console-log /tmp/cs.log` to capture agent logs) and fix; do NOT proceed until alpine pulls cleanly via the gcr path.

- [ ] **Step 8: Commit**

```bash
git add internal/agent/imagestore/store.go internal/agent/imagestore/gcrsource_test.go guest/init.sh
git commit -m "agent: wire CYBERSTACK_PULL_SOURCE env var to switch pull source

gcr → go-containerregistry adapter (HTTP/2, shared transport).
docker (default) → containers/image's docker.ParseReference (0.3 path).
guest/init.sh temporarily sets gcr to validate before flipping the default."
```

### Task 6: Phase A bench — gcr pull source against alpine, n=100

**Files:** none (output goes to `cyber-stack/benchmarks/`)

- [ ] **Step 1: Restart daemon clean**

In the daemon terminal (Ctrl-C the running one), restart plain:

```bash
~/dev/cyber5-io/cyber-stack/bin/cyberstackd
```

- [ ] **Step 2: Run the cold-pull bench**

```bash
~/dev/cyber5-io/cyber-stack/bin/cs-bench \
  -only cold-pull \
  -image mirror.gcr.io/library/alpine \
  -cold-pull-runs 100 \
  -output-dir ~/dev/cyber5-io/cyber-stack/benchmarks \
  -label cyber-0.4-gcr-vpn-off
```

Expected: a summary line. **Record P50.**

- [ ] **Step 3: Decide based on the number**

Compare the new P50 against the 0.3.0 baseline (860ms):

- **P50 ≤ 720ms** (≥140ms saved): hypothesis confirmed; gcr is the right source. Continue to Task 7.
- **P50 > 720ms** but < 860ms: partial win. Continue to Task 7 anyway — Phase B should add another 100–150ms.
- **P50 ≥ 860ms** (no improvement or regression): something is wrong. **Stop here**. Diagnose: re-add the phase tracer from the 0.3 git history and find out why gcr isn't faster. Possible causes: no transport reuse (singleton bug), per-request token re-fetch in g-c-r, or extra round trips we didn't anticipate.

- [ ] **Step 4: Commit the bench artefact**

```bash
git add benchmarks/cyber-0.4-gcr-vpn-off.json benchmarks/cyber-0.4-gcr-vpn-off.csv
git commit -m "bench: 0.4 phase A — gcr pull source vs 0.3.0 baseline

n=100 cold-pull runs, mirror.gcr.io/library/alpine, NordVPN off."
```

### Task 7: Smoke against tainer-* images, then make gcr the default + remove flag

**Files:**
- Modify: `cyber-stack/internal/agent/imagestore/store.go`
- Modify: `cyber-stack/guest/init.sh`

- [ ] **Step 1: Smoke pull each production image**

For each of `wordpress`, `nextjs`, `kompozi` (the riskiest images by size), pull via the daemon socket:

```bash
for img in tainer-wordpress tainer-nextjs tainer-kompozi; do
  echo "=== pulling $img ==="
  curl --unix-socket ~/.cyberstack/cyberstack.sock -X POST \
    "http://_/v1.43/images/create?fromImage=ghcr.io/cyber5-io/$img&tag=latest"
  echo
done
```

Expected: each prints a `Downloaded newer image for ...` line. If any fails, diagnose the gcr adapter — likely a missing manifest media-type case or auth path. **Do not proceed until all three pull successfully.**

- [ ] **Step 2: Flip the default + remove the env-var path**

Edit `internal/agent/imagestore/store.go`:

```go
func (s *Store) Pull(ctx context.Context, ref string) (*PullResult, error) {
	src, err := newGcrImageReference(ref)
	if err != nil {
		return nil, fmt.Errorf("parse src ref: %w", err)
	}
	dst, err := is.Transport.ParseStoreReference(s.cs, ref)
	if err != nil {
		return nil, fmt.Errorf("parse dst ref: %w", err)
	}

	policy, _ := signature.NewPolicyFromBytes([]byte(`{"default":[{"type":"insecureAcceptAnything"}]}`))
	polCtx, _ := signature.NewPolicyContext(policy)
	defer polCtx.Destroy()

	_, err = copy.Image(ctx, polCtx, dst, src, &copy.Options{
		SourceCtx: &types.SystemContext{},
	})
	if err != nil {
		return nil, fmt.Errorf("copy image: %w", err)
	}

	img, err := s.cs.Image(ref)
	if err != nil {
		return nil, fmt.Errorf("lookup pulled image: %w", err)
	}

	size, _ := s.cs.ImageSize(img.ID)
	return &PullResult{ImageID: img.ID, RepoTag: ref, Size: size}, nil
}
```

Delete the `newPullSource` function — gcr is now the only path. Remove the now-unused `os` import + the `docker` import if nothing else uses them (run `goimports -w` or remove manually).

Edit `guest/init.sh` to remove the temporary env var:

```sh
env GODEBUG=asyncpreemptoff=1 CYBERSTACK_UPLINK="$UPLINK" \
    /usr/bin/cyberstack-agent
```

- [ ] **Step 3: Run all unit tests**

```bash
go test ./internal/...
```

Expected: all green. The factory test (`TestGcrSourceFactoryReturnsCorrectType`) will fail because `newPullSource` is gone — delete it from `gcrsource_test.go`.

- [ ] **Step 4: Cross-compile + boot disk**

```bash
make boot-disk
```

Expected: builds clean.

- [ ] **Step 5: Smoke pull alpine one more time, no env var**

Restart daemon (Ctrl-C, restart). Pull:

```bash
curl --unix-socket ~/.cyberstack/cyberstack.sock -X POST \
  "http://_/v1.43/images/create?fromImage=mirror.gcr.io/library/alpine&tag=latest"
```

Expected: `Downloaded newer image for ...`. The gcr path now runs unconditionally.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/imagestore/store.go internal/agent/imagestore/gcrsource_test.go guest/init.sh
git commit -m "agent: make go-containerregistry the default pull source

Phase A bench validates the gcr path; the legacy docker.ParseReference path
is removed. tainer-{wordpress,nextjs,kompozi} all smoke-pull cleanly.
Removes the CYBERSTACK_PULL_SOURCE env-var bridge that gated this in
the previous commit."
```

---

## Phase B: in-process untar via vendored containers/storage fork

Five tasks, ~2–3 days. The key change is a one-line patch to `containers/storage`'s overlay graph driver, vendored via `go.mod replace`. Phase B builds on Phase A — both are active simultaneously after Task 12.

### Task 8: Vendor containers/storage and apply the untar patch

**Files:**
- Create: `cyber-stack/third_party/containers-storage/` (vendored copy of `containers/storage@v1.59.1`)
- Create: `cyber-stack/third_party/containers-storage/README.md`
- Modify: `cyber-stack/third_party/containers-storage/drivers/overlay/overlay.go` (one-line patch)
- Modify: `cyber-stack/go.mod` (add `replace` directive)

- [ ] **Step 1: Copy upstream into `third_party/`**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
mkdir -p third_party
cp -R "$(go env GOMODCACHE)/github.com/containers/storage@v1.59.1" third_party/containers-storage
chmod -R u+w third_party/containers-storage
```

Expected: `third_party/containers-storage/go.mod` exists; the module is at version `v1.59.1` per its go.mod.

- [ ] **Step 2: Patch the overlay driver**

Edit `third_party/containers-storage/drivers/overlay/overlay.go` line 48. Replace:

```go
var untar = chrootarchive.UntarUncompressed
```

with:

```go
// CyberStack 0.4 patch: bypass chrootarchive's reexec.Command fork. We're a
// single-tenant arm64 VM running as root with no untrusted multi-image input;
// the chroot security boundary doesn't justify ~100ms of fork overhead per
// pull. archive.UntarUncompressed has the same signature and same behaviour
// minus the chroot. See ../README.md for the trade-off.
var untar = archive.UntarUncompressed
```

Verify the `archive` import is already present in this file:

```bash
grep -n '"github.com/containers/storage/pkg/archive"' third_party/containers-storage/drivers/overlay/overlay.go
```

Expected: a match (already imported by the surrounding code). If not present, add it to the import block.

- [ ] **Step 3: Add `replace` directive + `require` of the local path**

Edit `cyber-stack/go.mod`. Add at the end:

```go
replace github.com/containers/storage => ./third_party/containers-storage
```

Also confirm there's a `require github.com/containers/storage v1.59.1` line; if not, `go mod tidy` will add it once we build.

- [ ] **Step 4: Run go mod tidy + verify**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
go mod tidy
go build ./...
```

Expected: builds clean. If you get import errors, the local copy is missing something — verify all `pkg/` subdirs were copied and `go.mod` inside `third_party/containers-storage/` is intact.

- [ ] **Step 5: Write the README**

Create `cyber-stack/third_party/containers-storage/README.md`:

```markdown
# containers/storage — CyberStack fork

This is a vendored copy of `github.com/containers/storage@v1.59.1` with **one** intentional patch.

## The patch

`drivers/overlay/overlay.go`, line 48:

```diff
-var untar = chrootarchive.UntarUncompressed
+var untar = archive.UntarUncompressed
```

`chrootarchive.UntarUncompressed` re-execs the agent binary with different argv to apply image layers inside a chroot. `archive.UntarUncompressed` does the same work in-process. Same function signature, same tar-handling code path; the only difference is the chroot security boundary.

## Why we ship the patch

CyberStack's pull pipeline gets called inside a single-tenant arm64 VM running as root. The chroot's purpose is to limit the blast radius of a malicious tar (zip-slip, device nodes, hardlink escapes); in our threat model we don't pull from untrusted registries, and the VM is disposable and isolated from the host filesystem by virtio-blk.

The fork overhead measured ~100–150ms per pull during 0.3 phase tracing. See `cyber-stack/docs/benchmarking.md` "0.3.0 results" for the budget breakdown that motivated this change.

## Forward-porting

When upstream `containers/storage` ships a new version we want to pick up:

1. `cd third_party && rm -rf containers-storage`
2. `cp -R "$(go env GOMODCACHE)/github.com/containers/storage@<NEW_VERSION>" containers-storage`
3. Re-apply the one-line patch above (`drivers/overlay/overlay.go` near `var untar`).
4. Update `go.mod` `require` line.
5. `go mod tidy && go build ./... && go test ./internal/...`.

The patch is small and stable; if upstream ever changes how the overlay driver dispatches untar, re-evaluate the threat model rather than mechanically re-applying.

## Reverting

To restore upstream behaviour:

1. Delete the `replace` directive from `cyber-stack/go.mod`.
2. `go mod tidy`.
3. Optionally remove `third_party/containers-storage/`.
```

- [ ] **Step 6: Commit**

```bash
git add third_party/ go.mod go.sum
git commit -m "third_party: vendor containers/storage with in-process untar patch

Single-line patch to drivers/overlay/overlay.go swapping
chrootarchive.UntarUncompressed for archive.UntarUncompressed. Eliminates
the per-pull reexec.Command fork that was costing ~100-150ms in 0.3 phase
tracing.

Trade-off: gives up the chroot security boundary around tar extraction.
Acceptable in a single-tenant arm64 VM running as root with disposable
state. Documented in third_party/containers-storage/README.md."
```

### Task 9: Verify the patch is active (no chrootarchive reexec at runtime)

**Files:** none (runtime check)

- [ ] **Step 1: Cross-compile + boot disk**

```bash
make boot-disk
```

- [ ] **Step 2: Restart daemon, smoke pull alpine, capture agent logs**

In daemon terminal:
```bash
~/dev/cyber5-io/cyber-stack/bin/cyberstackd -console-log /tmp/cs-untar.log
```

In another terminal:
```bash
curl --unix-socket ~/.cyberstack/cyberstack.sock -X POST \
  "http://_/v1.43/images/create?fromImage=mirror.gcr.io/library/alpine&tag=latest"
```

Expected: pull succeeds.

- [ ] **Step 3: Confirm reexec did NOT fire**

The chrootarchive reexec call would log `apply layer in chroot` or similar messages. With our patch, the in-process untar runs in the agent's main goroutine — no fork. Check by counting agent processes during a pull:

```bash
# In a third terminal, while a pull is running:
ps aux | grep cyberstack-agent | grep -v grep | wc -l
```

Expected: `1` (the long-running agent). With chrootarchive, you'd briefly see 2+ during ApplyDiff.

- [ ] **Step 4: Confirm chrootarchive isn't even imported by the live binary**

```bash
go tool nm bin/cyberstack-agent-linux-arm64 | grep chrootarchive | wc -l
```

Expected: `0` or near-zero. With the patch active, the overlay driver no longer references `chrootarchive`; the linker should drop most of it (a few helpers from other packages might transitively pull it in, but the bulk goes).

If non-zero and growing, the patch isn't active — investigate `go.mod` and rerun `go mod tidy`.

- [ ] **Step 5: No code change in this task — just runtime verification. Skip the commit step.**

### Task 10: Phase B bench — both halves active

**Files:** none (output goes to `cyber-stack/benchmarks/`)

- [ ] **Step 1: Restart daemon**

```bash
~/dev/cyber5-io/cyber-stack/bin/cyberstackd
```

- [ ] **Step 2: Run cold-pull bench**

```bash
~/dev/cyber5-io/cyber-stack/bin/cs-bench \
  -only cold-pull \
  -image mirror.gcr.io/library/alpine \
  -cold-pull-runs 100 \
  -output-dir ~/dev/cyber5-io/cyber-stack/benchmarks \
  -label cyber-0.4-untar-vpn-off
```

Expected: a summary line. **Record P50.**

- [ ] **Step 3: Decide based on the number**

Compare the new P50 against:
- 0.3.0 baseline: 860ms
- Phase-A-only (Task 6 result): your `cyber-0.4-gcr-vpn-off` P50

Phase A + B target: ≤520ms.

- **P50 ≤ 520ms**: hits target. Continue to Task 11 (final ship work).
- **520ms < P50 ≤ 620ms**: close. Continue to Task 11; the final 100ms might come out in cleanup, or we accept the slight miss.
- **P50 > 620ms**: stop. Investigate why neither phase delivered the expected savings. Re-add the phase tracer (it lived at `internal/agent/imagestore/store.go` before commit `30de8de`; revert that file, rebuild, re-bench, see where the time went). Decide whether to keep digging or ship 0.4 with the gap and queue a 0.5 for further work.

- [ ] **Step 4: Commit the bench artefact**

```bash
git add benchmarks/cyber-0.4-untar-vpn-off.json benchmarks/cyber-0.4-untar-vpn-off.csv
git commit -m "bench: 0.4 phase B — gcr + in-process untar combined

n=100 cold-pull runs, mirror.gcr.io/library/alpine, NordVPN off.
Both halves of the 0.4 spec active simultaneously."
```

### Task 11: Smoke pull all tainer-* images with combined stack

**Files:** none (validation only)

- [ ] **Step 1: Pull each production image**

```bash
for img in tainer-wordpress tainer-php tainer-nodejs tainer-nextjs tainer-nuxtjs tainer-nestjs tainer-react tainer-kompozi; do
  echo "=== pulling $img ==="
  curl --unix-socket ~/.cyberstack/cyberstack.sock -X POST \
    "http://_/v1.43/images/create?fromImage=ghcr.io/cyber5-io/$img&tag=latest"
  echo
done
```

Expected: each prints a `Downloaded newer image for ...` line. The full set is broader than Phase A's smoke (which used just 3) because we're committing the patch to ship and want full coverage.

- [ ] **Step 2: Run a tainer-wordpress container to verify the layer-apply produced a valid rootfs**

```bash
curl --unix-socket ~/.cyberstack/cyberstack.sock -X POST \
  -H "Content-Type: application/json" \
  -d '{"Image":"ghcr.io/cyber5-io/tainer-wordpress:latest","Cmd":["true"]}' \
  "http://_/v1.43/containers/create?name=cs04-smoke"
curl --unix-socket ~/.cyberstack/cyberstack.sock -X POST \
  "http://_/v1.43/containers/cs04-smoke/start"
curl --unix-socket ~/.cyberstack/cyberstack.sock -X POST \
  "http://_/v1.43/containers/cs04-smoke/wait"
curl --unix-socket ~/.cyberstack/cyberstack.sock -X DELETE \
  "http://_/v1.43/containers/cs04-smoke"
```

Expected: container created, started, waits with exit code 0, deletes cleanly. If `true` exits non-zero or crun complains about the rootfs, the in-process untar produced a broken filesystem — investigate with `docker exec` on a sleeping container to inspect the rootfs.

- [ ] **Step 3: No commit — validation only.**

### Task 12: Run the full 5-budget bench against 0.3 (regression check)

**Files:** none (validation only)

- [ ] **Step 1: Run the full bench**

```bash
~/dev/cyber5-io/cyber-stack/bin/cs-bench \
  -runs 20 \
  -cold-pull-runs 100 \
  -image mirror.gcr.io/library/alpine \
  -output-dir ~/dev/cyber5-io/cyber-stack/benchmarks \
  -label cyber-0.4-full-vpn-off
```

Expected: warm-run P50 < 500ms, exec P50 < 200ms, cold-pull P50 ≤ 520ms (target), cold-cold P50 < 6s, idle < 200MB.

- [ ] **Step 2: Decide**

- **All five PASS**: continue to Task 13.
- **Any other budget regressed**: investigate. Most likely culprits: warm-run could have a minor hit if the gcr adapter does extra work for cached images (it shouldn't, but verify). cold-cold could regress if the new boot disk size moved (unlikely, the patch is one line).

- [ ] **Step 3: Commit the bench artefact**

```bash
git add benchmarks/cyber-0.4-full-vpn-off.json benchmarks/cyber-0.4-full-vpn-off.csv
git commit -m "bench: 0.4 full 5-budget regression check

Confirms warm-run, exec, cold-cold, idle remain green alongside the
cold-pull win."
```

---

## Phase C: ship

### Task 13: Bump version + update benchmarking.md

**Files:**
- Modify: `cyber-stack/internal/httpapi/version.go`
- Modify: `cyber-stack/docs/benchmarking.md`

- [ ] **Step 1: Bump the version constant**

Edit `cyber-stack/internal/httpapi/version.go`:

```go
var CyberStackVersion = "0.4.0"
```

- [ ] **Step 2: Add 0.4.0 results to benchmarking.md**

Append after the existing "0.3.0 results" section, before "Troubleshooting":

```markdown
## 0.4.0 results

Captured 2026-MM-DD against `mirror.gcr.io/library/alpine:latest`, NordVPN off, n=100. (Replace MM-DD with the actual run date.)

| metric | OrbStack | CyberStack 0.3.0 | CyberStack 0.4.0 | gap (vs OrbStack) |
|---|---|---|---|---|
| cold-pull P50 | 467ms | 860ms | <FILL_IN>ms | +<FILL_IN>ms |
| cold-pull P90 | 521ms | 965ms | <FILL_IN>ms | +<FILL_IN>ms |
| cold-pull P99 | 686ms | 1083ms | <FILL_IN>ms | +<FILL_IN>ms |
| cold-pull mean | 472ms | 873ms | <FILL_IN>ms | +<FILL_IN>ms |

Replace the `<FILL_IN>` placeholders with the values from `benchmarks/cyber-0.4-full-vpn-off.json`'s cold-pull section.

### What changed

Two surgical interventions, sized from the 0.3 phase-tracer evidence:

1. **`go-containerregistry` adapter** as the source side of `copy.Image`. Process-singleton `*http.Transport` with `ForceAttemptHTTP2: true` and `MaxIdleConnsPerHost: 50`. Connection reuse + HTTP/2 multiplexing across pulls cuts the pre-phase round-trip count and amortises TLS handshakes.
2. **`go.mod replace` of `containers/storage`** with a one-line patch swapping `chrootarchive.UntarUncompressed` for `archive.UntarUncompressed` in the overlay graph driver. Eliminates the `reexec.Command` fork that was costing ~100–150ms per pull.

The destination side, the daemon ↔ agent vsock plumbing, the boot-disk pipeline, and `containers/image` itself are all unchanged from 0.3.0.
```

- [ ] **Step 3: Run the full test suite and build to confirm clean**

```bash
go test ./internal/...
make build
```

Expected: tests green, both binaries produced.

- [ ] **Step 4: Commit**

```bash
git add internal/httpapi/version.go docs/benchmarking.md
git commit -m "docs: 0.4.0 results + version bump"
```

### Task 14: Tag v0.4.0 and update tainer-side docs

**Files:**
- Modify: `tainer/docs/superpowers/plans/2026-05-01-cyberstack-0.4-pull-pipeline.md` (this file — add a SHIPPED header)
- Modify: `tainer/docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md` (add a v0.4.0 status note)

- [ ] **Step 1: Add the SHIPPED header to this plan**

Insert after the first `# CyberStack 0.4 — ...` heading in this file, mirroring the 0.3 plan's pattern:

```markdown
> **Status: SHIPPED YYYY-MM-DD as `cyber-stack v0.4.0`.** Cold-pull P50 = <FILL_IN>ms over n=100 against mirror.gcr.io/library/alpine, <FILL_IN>ms <under/over> the 520ms target (vs OrbStack 467ms baseline). Other four budgets remain green. Two interventions landed: gcr-backed types.ImageSource adapter (shared *http.Transport, HTTP/2, MaxIdleConnsPerHost: 50), and a one-line patch to containers/storage's overlay driver swapping chrootarchive.UntarUncompressed → archive.UntarUncompressed (vendored via go.mod replace at third_party/containers-storage/). `containers/image` unchanged; vsock plumbing unchanged; boot-disk unchanged.
```

Replace `YYYY-MM-DD` and the `<FILL_IN>` values with actuals.

- [ ] **Step 2: Add v0.4.0 status note in master design**

In `tainer/docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md`, append a new status note paragraph below the existing 2026-05-01 (v0.3.0) one:

```markdown
> **Status note (YYYY-MM-DD):** CyberStack `v0.4.0` is live — pull-pipeline performance milestone shipped. Cold-pull P50 dropped from 860ms to <FILL_IN>ms (target ≤520ms). Two surgical changes: gcr-backed pull source for HTTP/2 + connection pooling (targets pre-phase round trips), one-line containers/storage patch for in-process untar (targets blob-copy fork overhead). See `docs/superpowers/plans/2026-05-01-cyberstack-0.4-pull-pipeline.md`. Persistent VM (suspend/resume) is the next milestone — sequenced as 0.5.
```

- [ ] **Step 3: Commit cyber-stack and tag**

```bash
cd /Users/lenineto/dev/cyber5-io/cyber-stack
git add -A   # picks up the cleanup if anything was missed
git status --short   # verify only intended files are staged
git commit -m "feat: ship v0.4.0 — pull pipeline performance"  # only if the prior commits left untracked work
git tag -a v0.4.0 -m "CyberStack v0.4.0 — pull pipeline performance"
git tag -l   # verify v0.4.0 appears
```

Expected: `v0.4.0` joins `v0.1.0`, `v0.2.0`, `v0.3.0` in the tag list.

- [ ] **Step 4: Commit tainer-side**

```bash
cd /Users/lenineto/dev/cyber5-io/tainer
git add docs/superpowers/plans/2026-05-01-cyberstack-0.4-pull-pipeline.md \
        docs/superpowers/specs/2026-04-23-tainer-rebuild-cyberstack-design.md
git commit -m "docs: Mark CyberStack 0.4.0 as live"
```

- [ ] **Step 5: No push — both tag and commits are local until the user runs `git push --tags origin main` themselves.**

### Task 15: Final validation pass

**Files:** none

- [ ] **Step 1: Verify both repos clean**

```bash
git -C /Users/lenineto/dev/cyber5-io/cyber-stack status --short
git -C /Users/lenineto/dev/cyber5-io/tainer status --short
```

Expected: both empty.

- [ ] **Step 2: Verify the v0.4.0 tag points at the right commit**

```bash
git -C /Users/lenineto/dev/cyber5-io/cyber-stack show --stat v0.4.0 | head -3
```

Expected: tag exists; tagged commit subject contains `0.4.0`.

- [ ] **Step 3: Make and run from a fresh clone (smoke test the build)**

```bash
cd /tmp
rm -rf cs-fresh-build
git clone /Users/lenineto/dev/cyber5-io/cyber-stack cs-fresh-build
cd cs-fresh-build
git checkout v0.4.0
make boot-disk
ls -la bin/cyberstackd guest/cyberstack-boot-arm64.img
```

Expected: builds clean from scratch on the tagged commit. Confirms no path-dependent state was left in the working tree.

- [ ] **Step 4: No commit — validation only.**

---

## Self-review

- **Spec coverage.** Spec sections mapped to tasks: Goal → Task 12 acceptance check; Evidence → quoted in plan header; Locked decisions → Tasks 1–8 implement them; Estimated savings → Tasks 6, 10 measure them; Architecture/Layer 1 → Tasks 1–7; Architecture/Layer 2 → Task 8; Build sequencing → Phase A then Phase B then Phase C; Risks → mitigations baked in (smoke tests in Tasks 7 and 11; full-budget bench in Task 12; abort gates in Tasks 6 and 10); Out of scope → not implemented; Acceptance criteria → Tasks 11–15 cover all five.
- **Placeholder scan.** The `<FILL_IN>` markers in Tasks 13–14 are unavoidable — they're values we can't know until the bench runs. They are marked clearly and the engineer is told what to substitute. No "TBD" / "TODO" / "implement later" / "appropriate error handling" patterns.
- **Type consistency.** `gcrImageReference` and `gcrImageSource` types named consistently across all tasks. `newGcrImageReference` and `newPullSource` (the latter only exists in Tasks 5–7) have stable signatures. `SharedTransport()` returns `http.RoundTripper` consistently. `var untar = archive.UntarUncompressed` matches between Task 8 patch and Task 9 verification text.

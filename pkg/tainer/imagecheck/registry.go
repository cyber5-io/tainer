// Package imagecheck compares local images against their registry counterparts.
package imagecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrNotFound is returned when the registry answers 404 for a manifest.
var ErrNotFound = errors.New("image not found in registry")

// Ref is a parsed image reference.
type Ref struct {
	Host, Repo, Tag string
}

// ParseRef splits "host/repo[:tag]" into its parts; the tag defaults to latest.
func ParseRef(s string) (Ref, error) {
	if strings.Contains(s, "@") {
		return Ref{}, fmt.Errorf("digest references are not supported: %q", s)
	}
	host, rest, ok := strings.Cut(s, "/")
	if !ok || host == "" || rest == "" {
		return Ref{}, fmt.Errorf("invalid image reference %q", s)
	}
	repo, tag := rest, "latest"
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		repo, tag = rest[:i], rest[i+1:]
	}
	if repo == "" || tag == "" {
		return Ref{}, fmt.Errorf("invalid image reference %q", s)
	}
	return Ref{Host: host, Repo: repo, Tag: tag}, nil
}

const acceptManifests = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

type manifestDoc struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
		} `json:"platform"`
	} `json:"manifests"`
}

// RemoteConfigDigest returns the config digest (sha256:...) of the linux/<arch>
// image behind ref. An empty arch means arm64.
func RemoteConfigDigest(ctx context.Context, httpc *http.Client, ref Ref, arch string) (string, error) {
	return remoteConfigDigest(ctx, httpc, "https", ref, arch)
}

func remoteConfigDigest(ctx context.Context, httpc *http.Client, scheme string, ref Ref, arch string) (string, error) {
	if arch == "" {
		arch = "arm64"
	}
	c := &regClient{httpc: httpc, scheme: scheme, ref: ref}
	doc, err := c.manifest(ctx, ref.Tag)
	if err != nil {
		return "", err
	}
	if len(doc.Manifests) == 0 {
		if doc.Config.Digest == "" {
			return "", fmt.Errorf("manifest for %s/%s:%s has no config digest", ref.Host, ref.Repo, ref.Tag)
		}
		return doc.Config.Digest, nil
	}
	for _, m := range doc.Manifests {
		if m.Platform.Architecture == arch && m.Platform.OS == "linux" {
			sub, err := c.manifest(ctx, m.Digest)
			if err != nil {
				return "", err
			}
			if sub.Config.Digest == "" {
				return "", fmt.Errorf("manifest %s has no config digest", m.Digest)
			}
			return sub.Config.Digest, nil
		}
	}
	return "", fmt.Errorf("no linux/%s image for %s/%s:%s", arch, ref.Host, ref.Repo, ref.Tag)
}

type regClient struct {
	httpc  *http.Client
	scheme string
	ref    Ref
	token  string
}

func (c *regClient) manifest(ctx context.Context, reference string) (*manifestDoc, error) {
	u := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.scheme, c.ref.Host, c.ref.Repo, reference)
	resp, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		resp.Body.Close()
		if err := c.fetchToken(ctx, challenge); err != nil {
			return nil, err
		}
		if resp, err = c.get(ctx, u); err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("registry returned %s for %s", resp.Status, u)
	}
	var doc manifestDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &doc, nil
}

func (c *regClient) get(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", acceptManifests)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.httpc.Do(req)
}

func (c *regClient) fetchToken(ctx context.Context, challenge string) error {
	scheme, params, _ := strings.Cut(challenge, " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return fmt.Errorf("unsupported registry auth challenge %q", challenge)
	}
	attrs := parseChallenge(params)
	realm := attrs["realm"]
	if realm == "" {
		return fmt.Errorf("registry auth challenge has no realm: %q", challenge)
	}
	tu, err := url.Parse(realm)
	if err != nil {
		return fmt.Errorf("bad auth realm %q: %w", realm, err)
	}
	q := tu.Query()
	if v := attrs["service"]; v != "" {
		q.Set("service", v)
	}
	if v := attrs["scope"]; v != "" {
		q.Set("scope", v)
	}
	tu.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tu.String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry token endpoint returned %s", resp.Status)
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil {
		return fmt.Errorf("decode token: %w", err)
	}
	c.token = tok.Token
	if c.token == "" {
		c.token = tok.AccessToken
	}
	if c.token == "" {
		return errors.New("registry token response had no token")
	}
	return nil
}

// parseChallenge parses key="value" pairs from a WWW-Authenticate parameter list.
func parseChallenge(s string) map[string]string {
	out := map[string]string{}
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimLeft(s, ", ") {
		k, rest, ok := strings.Cut(s, "=")
		if !ok {
			break
		}
		k = strings.TrimSpace(k)
		var v string
		if strings.HasPrefix(rest, `"`) {
			end := strings.Index(rest[1:], `"`)
			if end < 0 {
				break
			}
			v, s = rest[1:1+end], rest[end+2:]
		} else {
			v, s, _ = strings.Cut(rest, ",")
		}
		out[strings.ToLower(k)] = v
	}
	return out
}

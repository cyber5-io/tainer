package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type tick struct{ done, total int64 }

// chunkReader hands out its data a fixed number of bytes per Read, like a
// network body arriving in pieces.
type chunkReader struct {
	data  []byte
	chunk int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(r.chunk, len(p), len(r.data))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

func TestProgressWriterReportsZeroHalfAndFull(t *testing.T) {
	var got []tick
	w := newProgressWriter(100, func(done, total int64) { got = append(got, tick{done, total}) })
	if _, err := io.Copy(io.Discard, io.TeeReader(&chunkReader{data: make([]byte, 100), chunk: 50}, w)); err != nil {
		t.Fatal(err)
	}
	want := []tick{{0, 100}, {50, 100}, {100, 100}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestProgressWriterWithoutReporterIsSilent(t *testing.T) {
	w := newProgressWriter(100, nil)
	if n, err := w.Write(make([]byte, 10)); n != 10 || err != nil {
		t.Errorf("n=%d err=%v", n, err)
	}
}

func TestDownloadWithProgressReportsTheBody(t *testing.T) {
	body := strings.Repeat("x", 4096)
	sum := sha256.Sum256([]byte(body))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TAINER_RELEASE_URL", srv.URL)
	url := srv.URL + "/tainer.pkg"
	var got []tick
	_, err := DownloadWithProgress(context.Background(), Artifact{URL: url, SHA256: hex.EncodeToString(sum[:])}, t.TempDir(),
		func(done, total int64) { got = append(got, tick{done, total}) })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || got[0] != (tick{0, 4096}) || got[len(got)-1] != (tick{4096, 4096}) {
		t.Errorf("reports %v", got)
	}
}

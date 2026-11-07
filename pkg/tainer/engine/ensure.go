package engine

import (
	"context"
	"fmt"
)

// DownloadError reports an image that is not stored locally and could not be
// downloaded. Its message is plain user-facing copy; the underlying cause is
// available through Unwrap for debugging.
type DownloadError struct {
	Ref   string
	Cause error
}

func (e *DownloadError) Error() string {
	return fmt.Sprintf("couldn't download %s — are you offline?", e.Ref)
}

func (e *DownloadError) Unwrap() error { return e.Cause }

// EnsureImages downloads only the refs that has reports as missing. Images
// already stored locally are never pulled, so everything keeps starting
// offline; `tainer update` is how newer images arrive.
func EnsureImages(ctx context.Context, refs []string,
	has func(context.Context, string) bool, pull func(context.Context, string) error,
) error {
	for _, ref := range refs {
		if has(ctx, ref) {
			continue
		}
		if err := pull(ctx, ref); err != nil {
			return &DownloadError{Ref: ref, Cause: err}
		}
	}
	return nil
}

// EnsureImages pulls only the refs not already stored locally.
func (c *Client) EnsureImages(ctx context.Context, refs ...string) error {
	has := func(ctx context.Context, ref string) bool {
		_, err := c.ImageInspect(ctx, ref)
		return err == nil
	}
	return EnsureImages(ctx, refs, has, c.Pull)
}

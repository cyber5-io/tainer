package engine

import (
	"context"
	"errors"
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

// MissingImages returns the refs that has reports as absent locally. A has
// error means the runtime could not answer; it is returned untouched rather
// than treated as "missing".
func MissingImages(ctx context.Context, refs []string,
	has func(context.Context, string) (bool, error),
) ([]string, error) {
	var missing []string
	for _, ref := range refs {
		ok, err := has(ctx, ref)
		if err != nil {
			return nil, err
		}
		if !ok {
			missing = append(missing, ref)
		}
	}
	return missing, nil
}

// EnsureImages downloads only the refs that has reports as missing. Images
// already stored locally are never pulled, so everything keeps starting
// offline; `tainer update` is how newer images arrive.
func EnsureImages(ctx context.Context, refs []string,
	has func(context.Context, string) (bool, error), pull func(context.Context, string) error,
) error {
	missing, err := MissingImages(ctx, refs, has)
	if err != nil {
		return err
	}
	for _, ref := range missing {
		if err := pull(ctx, ref); err != nil {
			return &DownloadError{Ref: ref, Cause: err}
		}
	}
	return nil
}

// PullImages pulls every ref, whether stored locally or not, so moving tags
// resolve to their current image. It is for a pod about to be recreated;
// an ordinary start uses EnsureImages and never contacts the registry for
// an image it already has. The first failure stops it as a DownloadError.
func PullImages(ctx context.Context, refs []string, pull func(context.Context, string) error) error {
	for _, ref := range refs {
		if err := pull(ctx, ref); err != nil {
			return &DownloadError{Ref: ref, Cause: err}
		}
	}
	return nil
}

func (c *Client) hasImage(ctx context.Context, ref string) (bool, error) {
	_, err := c.ImageInspect(ctx, ref)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrImageNotFound) {
		return false, nil
	}
	return false, err
}

// MissingImages returns the refs not stored locally.
func (c *Client) MissingImages(ctx context.Context, refs ...string) ([]string, error) {
	return MissingImages(ctx, refs, c.hasImage)
}

// EnsureImages pulls only the refs not already stored locally.
func (c *Client) EnsureImages(ctx context.Context, refs ...string) error {
	return EnsureImages(ctx, refs, c.hasImage, c.Pull)
}

// PullImages pulls every ref, including ones already stored locally.
func (c *Client) PullImages(ctx context.Context, refs ...string) error {
	return PullImages(ctx, refs, c.Pull)
}

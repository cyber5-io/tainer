package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// ExecResult bundles the captured output of a one-shot exec.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

// Exec runs cmd inside the named container and waits for it to exit,
// returning captured stdout/stderr and the exit code. For a streaming
// or interactive variant, build it on top of ContainerExecAttach later
// — keep this helper focused on the "run-and-collect" case that
// project/router use today.
func (c *Client) Exec(ctx context.Context, name string, cmd []string) (ExecResult, error) {
	createResp, err := c.api.ContainerExecCreate(ctx, name, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("engine: exec create on %s: %w", name, err)
	}

	hijack, err := c.api.ContainerExecAttach(ctx, createResp.ID, container.ExecStartOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("engine: exec attach on %s: %w", name, err)
	}
	defer hijack.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, hijack.Reader); err != nil {
		return ExecResult{}, fmt.Errorf("engine: exec read on %s: %w", name, err)
	}

	insp, err := c.api.ContainerExecInspect(ctx, createResp.ID)
	if err != nil {
		return ExecResult{}, fmt.Errorf("engine: exec inspect on %s: %w", name, err)
	}

	return ExecResult{
		ExitCode: insp.ExitCode,
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
	}, nil
}

// ExecStreamOptions configures a streaming exec session for
// interactive commands (bash, wp shell, artisan tinker) and for
// long-running commands whose output should reach the user live.
//
// When Tty is true, Stdout and Stderr streams are merged in the
// container so we only bind Stdout; call sites should still set
// Stderr in case a legacy container doesn't honour the request.
type ExecStreamOptions struct {
	Cmd     []string
	User    string    // optional: --user
	WorkDir string    // optional: --workdir
	Env     []string  // optional: extra env vars (KEY=VAL)
	Tty     bool      // allocate a container-side TTY
	Stdin   io.Reader // nil to skip stdin attach
	Stdout  io.Writer // required
	Stderr  io.Writer // required

	// OnAttached, when non-nil and Tty is true, is called once the
	// exec is attached, with a function that resizes the remote TTY.
	// The caller owns sending the initial size and re-sending on
	// SIGWINCH; resize errors are advisory (an older daemon without
	// the resize endpoint just leaves the terminal at its default
	// size).
	OnAttached func(resize func(width, height uint16) error)
}

// ExecStream runs cmd inside the named container and streams stdio
// live. Returns the exit code when the command finishes. Blocks until
// the command exits OR ctx is cancelled.
//
// Design notes:
//   - Distinct from Exec/ExecWithStdin (which buffer output) so
//     internal short commands (db.dump, router.update) don't change.
//   - When Tty is true the container writes a single interleaved
//     byte stream to Stdout — no stdcopy demux, no Stderr split.
//   - Stdin copy runs in a goroutine; we CloseWrite() when it finishes
//     so the container sees EOF on stdin (essential for commands like
//     `cat` reading from a pipe).
func (c *Client) ExecStream(ctx context.Context, name string, opts ExecStreamOptions) (int, error) {
	if opts.Stdout == nil || opts.Stderr == nil {
		return 1, fmt.Errorf("engine: ExecStream needs Stdout and Stderr")
	}

	trace := func(string) {}
	if os.Getenv("TAINER_EXEC_TRACE") != "" {
		t0 := time.Now()
		trace = func(phase string) {
			fmt.Fprintf(os.Stderr, "[trace +%6.0fms] %s\n", time.Since(t0).Seconds()*1000, phase)
		}
	}
	trace("create")
	createResp, err := c.api.ContainerExecCreate(ctx, name, container.ExecOptions{
		Cmd:          opts.Cmd,
		User:         opts.User,
		WorkingDir:   opts.WorkDir,
		Env:          opts.Env,
		Tty:          opts.Tty,
		AttachStdin:  opts.Stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return 1, fmt.Errorf("engine: exec create on %s: %w", name, err)
	}
	trace("created")

	hijack, err := c.api.ContainerExecAttach(ctx, createResp.ID, container.ExecStartOptions{
		Tty: opts.Tty,
	})
	if err != nil {
		return 1, fmt.Errorf("engine: exec attach on %s: %w", name, err)
	}
	defer hijack.Close()
	trace("attached")

	if opts.Tty && opts.OnAttached != nil {
		execID := createResp.ID
		opts.OnAttached(func(width, height uint16) error {
			return c.api.ContainerExecResize(ctx, execID, container.ResizeOptions{
				Height: uint(height),
				Width:  uint(width),
			})
		})
	}

	// stdin pump: block-copy user stdin into the hijacked conn until
	// EOF. When we finish, CloseWrite() so the container sees EOF —
	// otherwise commands like `cat` block forever.
	stdinDone := make(chan struct{})
	if opts.Stdin != nil {
		go func() {
			defer close(stdinDone)
			_, _ = io.Copy(hijack.Conn, opts.Stdin)
			_ = hijack.CloseWrite()
		}()
	} else {
		close(stdinDone)
	}

	// stdout+stderr pump: TTY mode is a single interleaved byte stream
	// (no docker frame header), non-TTY uses stdcopy to demux frames.
	outDone := make(chan error, 1)
	go func() {
		var err error
		if opts.Tty {
			_, err = io.Copy(opts.Stdout, hijack.Reader)
		} else {
			_, err = stdcopy.StdCopy(opts.Stdout, opts.Stderr, hijack.Reader)
		}
		outDone <- err
	}()

	// Wait for output stream to finish (that's the definitive signal
	// the exec is done producing bytes). Then inspect for exit code.
	trace("pumping")
	select {
	case err := <-outDone:
		if err != nil {
			// io.Copy returning an error mid-stream can still leave
			// meaningful exit-code info in the inspect call; log but
			// don't abort here.
			_ = err
		}
	case <-ctx.Done():
		return 1, ctx.Err()
	}
	// Best-effort: let any straggler stdin bytes finish flushing.
	// NOT in TTY mode: there stdin is the user's terminal, which never
	// EOFs — the session is over when the output stream ends (the
	// agent closed the pty), and waiting would hang until the user
	// pressed a key.
	if !opts.Tty {
		<-stdinDone
	}

	trace("output done")
	insp, err := c.api.ContainerExecInspect(ctx, createResp.ID)
	if err != nil {
		return 1, fmt.Errorf("engine: exec inspect on %s: %w", name, err)
	}
	trace("inspected")
	return insp.ExitCode, nil
}

// ExecWithStdin is like Exec but pipes stdin into the exec session.
// Used by `tainer db import`.
func (c *Client) ExecWithStdin(ctx context.Context, name string, cmd []string, stdin []byte) (ExecResult, error) {
	createResp, err := c.api.ContainerExecCreate(ctx, name, container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return ExecResult{}, err
	}
	hijack, err := c.api.ContainerExecAttach(ctx, createResp.ID, container.ExecStartOptions{})
	if err != nil {
		return ExecResult{}, err
	}
	defer hijack.Close()

	if _, err := hijack.Conn.Write(stdin); err != nil {
		return ExecResult{}, fmt.Errorf("write stdin: %w", err)
	}
	_ = hijack.CloseWrite()

	var out, errBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &errBuf, hijack.Reader); err != nil {
		return ExecResult{}, err
	}
	insp, err := c.api.ContainerExecInspect(ctx, createResp.ID)
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{ExitCode: insp.ExitCode, Stdout: out.Bytes(), Stderr: errBuf.Bytes()}, nil
}

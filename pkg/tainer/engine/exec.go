package engine

import (
	"bytes"
	"context"
	"fmt"

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

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/pod"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

// controlProvisionTimeout bounds the control pod bring-up that follows a
// runtime spawn. A first run pulls the control image, so it is generous; the
// user's own command waits for it.
const controlProvisionTimeout = 5 * time.Minute

// ensureControlPod is a seam for tests.
var ensureControlPod = pod.EnsureControlPod

func init() {
	runtime.OnSpawn = func(ctx context.Context, cli *engine.Client) {
		provisionControlPod(ctx, cli, os.Stderr)
	}
}

// provisionControlPod brings the control pod up after the runtime has been
// spawned. It never fails the user's command: on error it prints one line to
// w and the next runtime start retries.
func provisionControlPod(ctx context.Context, cli *engine.Client, w io.Writer) {
	// Own deadline, detached from the command's: a short-lived command's
	// context must not cut a first image pull off halfway.
	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), controlProvisionTimeout)
	defer cancel()
	if err := ensureControlPod(pctx, cli); err != nil {
		reason := strings.Join(strings.Fields(err.Error()), " ")
		fmt.Fprintf(w, "tainer: couldn't start the control dashboard yet (%s); it will retry next time tainer starts\n", reason)
	}
}

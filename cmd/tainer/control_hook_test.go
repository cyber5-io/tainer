package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
)

func TestControlHookIsRegistered(t *testing.T) {
	if runtime.OnSpawn == nil {
		t.Fatal("runtime.OnSpawn must be registered by the tainer command")
	}
}

func TestProvisionFailureWarnsAndDoesNotFailTheCommand(t *testing.T) {
	old := ensureControlPod
	t.Cleanup(func() { ensureControlPod = old })
	ensureControlPod = func(context.Context, *engine.Client) error {
		return errors.New("pod start: pull app: network is unreachable")
	}
	var stderr bytes.Buffer
	// The hook has no error return: whatever happens, the caller's command
	// carries on with the client runtime.Engine hands it.
	provisionControlPod(context.Background(), &engine.Client{}, &stderr)
	want := "tainer: couldn't start the control dashboard yet (pod start: pull app: network is unreachable); it will retry next time tainer starts\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q\nwant     %q", stderr.String(), want)
	}
}

func TestProvisionSuccessIsSilent(t *testing.T) {
	old := ensureControlPod
	t.Cleanup(func() { ensureControlPod = old })
	ensureControlPod = func(context.Context, *engine.Client) error { return nil }
	var stderr bytes.Buffer
	provisionControlPod(context.Background(), &engine.Client{}, &stderr)
	if stderr.Len() != 0 {
		t.Errorf("success must print nothing, got %q", stderr.String())
	}
}

package main

import (
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
)

func routerInspect(running bool, ip string) container.InspectResponse {
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: running}},
		NetworkSettings: &container.NetworkSettings{
			DefaultNetworkSettings: container.DefaultNetworkSettings{IPAddress: ip},
		},
	}
}

func TestRouterAddress(t *testing.T) {
	if got, err := routerAddress(routerInspect(true, "172.17.0.2"), nil); err != nil || got != "172.17.0.2" {
		t.Fatalf("running router = %q, %v", got, err)
	}
	for name, tc := range map[string]struct {
		insp container.InspectResponse
		err  error
	}{
		"missing":     {container.InspectResponse{}, errors.New("no such container")},
		"not running": {routerInspect(false, "172.17.0.2"), nil},
		"no address":  {routerInspect(true, ""), nil},
		"no state":    {container.InspectResponse{}, nil},
	} {
		_, err := routerAddress(tc.insp, tc.err)
		if err == nil || errCode(err) != "router-unavailable" || err.Error() != routerUnavailableMessage {
			t.Errorf("%s: err = %v (%s)", name, err, errCode(err))
		}
	}
}

func TestNetworkRouterJSONSnapshot(t *testing.T) {
	assertSnapshot(t, "network-router", func() { emitNetworkRouterJSON("172.17.0.2") })
}

func TestNetworkRouterUnavailableEnvelope(t *testing.T) {
	out, exit := failureHarness(t, true, func() {
		_, err := routerAddress(container.InspectResponse{}, errors.New("gone"))
		must(err)
	})
	want := `{"v":1,"error":{"code":"router-unavailable","message":"` + routerUnavailableMessage + `"}}` + "\n"
	if string(out) != want || exit != 1 {
		t.Errorf("out=%q exit=%d", out, exit)
	}
}

package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
	"github.com/cyber5-io/tainer/pkg/tainer/router"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
	"github.com/docker/docker/api/types/container"
)

const routerUnavailableMessage = "The tainer router is not running. Run tainer doctor."

// routerAddress turns the router's inspect record into its pod-network
// address. The control pod's preview service maps *.tainer.me to it, so a
// missing, stopped or address-less router is one stable error.
func routerAddress(insp container.InspectResponse, err error) (string, error) {
	unavailable := withCode("router-unavailable", errors.New(routerUnavailableMessage))
	if err != nil || insp.ContainerJSONBase == nil || insp.State == nil || !insp.State.Running {
		return "", unavailable
	}
	addr := engine.ContainerIP(insp)
	if addr == "" {
		return "", unavailable
	}
	return addr, nil
}

func runNetworkRouter(outMode tui.OutputMode) {
	eng, err := engine.New()
	must(err)
	defer eng.Close()
	ctx, cancel := ctxWithTimeout(10 * time.Second)
	defer cancel()
	addr, err := routerAddress(eng.Inspect(ctx, router.WebContainerName))
	must(err)
	switch outMode {
	case tui.ModeJSON:
		emitNetworkRouterJSON(addr)
	case tui.ModePlain:
		fmt.Println(addr)
	default:
		tui.Bookend("network")
		fmt.Println("  " + tui.MarkInfo() + "router: " + addr)
		fmt.Println()
		tui.BookendClose(0, "OK")
	}
}

func emitNetworkRouterJSON(address string) {
	jsonDoc(struct {
		Address string `json:"address"`
	}{Address: address})
}

//go:build darwin && amd64

package libkrun

import (
	"fmt"

	gvproxy "github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/podman/v6/pkg/machine/define"
	"github.com/containers/podman/v6/pkg/machine/ignition"
	"github.com/containers/podman/v6/pkg/machine/vmconfigs"
)

var errNotSupported = fmt.Errorf("libkrun is not supported on Intel Macs")

// LibKrunStubber is a no-op stub on amd64 — krunkit requires Apple Silicon.
type LibKrunStubber struct {
	vmconfigs.AppleHVConfig
}

func (l *LibKrunStubber) CreateVM(_ define.CreateVMOpts, _ *vmconfigs.MachineConfig, _ *ignition.IgnitionBuilder) error {
	return errNotSupported
}
func (l *LibKrunStubber) PrepareIgnition(_ *vmconfigs.MachineConfig, _ *ignition.IgnitionBuilder) (*ignition.ReadyUnitOpts, error) {
	return nil, errNotSupported
}
func (l *LibKrunStubber) Exists(_ string) (bool, error)        { return false, nil }
func (l *LibKrunStubber) MountType() vmconfigs.VolumeMountType { return 0 }
func (l *LibKrunStubber) MountVolumesToVM(_ *vmconfigs.MachineConfig, _ bool) error {
	return nil
}
func (l *LibKrunStubber) Remove(_ *vmconfigs.MachineConfig) ([]string, func() error, error) {
	return nil, func() error { return nil }, nil
}
func (l *LibKrunStubber) RemoveAndCleanMachines(_ *define.MachineDirs) error { return nil }
func (l *LibKrunStubber) SetProviderAttrs(_ *vmconfigs.MachineConfig, _ define.SetOptions) error {
	return nil
}
func (l *LibKrunStubber) StartNetworking(_ *vmconfigs.MachineConfig, _ *gvproxy.GvproxyCommand) error {
	return errNotSupported
}
func (l *LibKrunStubber) PostStartNetworking(_ *vmconfigs.MachineConfig, _ bool) error {
	return errNotSupported
}
func (l *LibKrunStubber) StartVM(_ *vmconfigs.MachineConfig) (func() error, func() error, error) {
	return nil, nil, errNotSupported
}
func (l *LibKrunStubber) State(_ *vmconfigs.MachineConfig, _ bool) (define.Status, error) {
	return "", errNotSupported
}
func (l *LibKrunStubber) StopVM(_ *vmconfigs.MachineConfig, _ bool) error          { return nil }
func (l *LibKrunStubber) StopHostNetworking(_ *vmconfigs.MachineConfig, _ define.VMType) error {
	return nil
}
func (l *LibKrunStubber) VMType() define.VMType                              { return define.LibKrun }
func (l *LibKrunStubber) UserModeNetworkEnabled(_ *vmconfigs.MachineConfig) bool { return false }
func (l *LibKrunStubber) UseProviderNetworkSetup() bool                      { return false }
func (l *LibKrunStubber) RequireExclusiveActive() bool                       { return false }
func (l *LibKrunStubber) UpdateSSHPort(_ *vmconfigs.MachineConfig, _ int) error   { return nil }
func (l *LibKrunStubber) GetRosetta(_ *vmconfigs.MachineConfig) (bool, error)     { return false, nil }

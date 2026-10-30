package router

import (
	"net"
	"testing"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, p, _ := net.SplitHostPort(l.Addr().String())
	l.Close()
	return p
}

func listenWithBanner(t *testing.T, banner string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(banner))
			c.Close()
		}
	}()
	_, p, _ := net.SplitHostPort(l.Addr().String())
	return p
}

func TestSSHPortFallback(t *testing.T) {
	free := freePort(t)
	taken := listenWithBanner(t, "SSH-2.0-OpenSSH_9.6\r\n")
	const fallback = "2222"

	if got := chooseSSHPort(free, fallback); got != free {
		t.Errorf("free preferred port: got %s, want %s", got, free)
	}
	if got := chooseSSHPort(taken, fallback); got != fallback {
		t.Errorf("taken preferred port: got %s, want fallback %s", got, fallback)
	}
}

func TestSSHPortAvailableTreatsOwnRouterAsAvailable(t *testing.T) {
	ours := listenWithBanner(t, "SSH-2.0-SSHPiper\r\n")
	if !sshPortAvailable(ours) {
		t.Error("a port held by our own sshpiper router must count as available")
	}
}

func TestAdoptSSHPort(t *testing.T) {
	sshPortMu.Lock()
	prevVal, prevSet := sshPortVal, sshPortSet
	sshPortMu.Unlock()
	t.Cleanup(func() {
		sshPortMu.Lock()
		sshPortVal, sshPortSet = prevVal, prevSet
		sshPortMu.Unlock()
	})

	adoptSSHPort(22)
	if got := SSHHostPort(); got != 22 {
		t.Errorf("after adopt(22), SSHHostPort() = %d", got)
	}
	adoptSSHPort(2222)
	if got := SSHHostPort(); got != 2222 {
		t.Errorf("after adopt(2222), SSHHostPort() = %d", got)
	}
}

func TestPublishesHostPort(t *testing.T) {
	hostPorts := []string{"22"}
	if !publishesHostPort(hostPorts, 22) || publishesHostPort(hostPorts, 2222) {
		t.Error("publishesHostPort mismatch")
	}
}

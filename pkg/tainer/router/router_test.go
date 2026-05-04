package router

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/pod"
)

func TestRouterContainerNamesMatchPodPackage(t *testing.T) {
	if WebContainerName != pod.RouterWebName {
		t.Errorf("WebContainerName drift: %q vs pod.RouterWebName %q", WebContainerName, pod.RouterWebName)
	}
	if SSHContainerName != pod.RouterSSHName {
		t.Errorf("SSHContainerName drift: %q vs pod.RouterSSHName %q", SSHContainerName, pod.RouterSSHName)
	}
}

func TestExtraHTTPPortsCollects(t *testing.T) {
	pods := []PodEndpoint{
		{Pod: "mywp", WebIP: "10.42.7.4", HTTPServices: []HTTPService{{Role: "mail", Port: 8025, IP: "10.42.7.5"}}},
		{Pod: "mynext", WebIP: "10.42.8.4", HTTPServices: []HTTPService{{Role: "mail", Port: 8025, IP: "10.42.8.5"}, {Role: "phpmyadmin", Port: 9090, IP: "10.42.8.6"}}},
	}
	got := ExtraHTTPPorts(pods)
	want := []int{8025, 9090}
	if len(got) != len(want) {
		t.Fatalf("ports count: got %v, want %v", got, want)
	}
	for i, p := range got {
		if p != want[i] {
			t.Errorf("port[%d]: got %d, want %d", i, p, want[i])
		}
	}
}

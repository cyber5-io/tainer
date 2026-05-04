package pod

import "testing"

func TestNetworkName(t *testing.T) {
	if got := NetworkName("mywp"); got != "tainer-mywp" {
		t.Errorf("NetworkName: got %q, want tainer-mywp", got)
	}
}

func TestContainerName(t *testing.T) {
	if got := ContainerName("mywp", "web"); got != "tainer-mywp-web" {
		t.Errorf("ContainerName: got %q, want tainer-mywp-web", got)
	}
}

func TestRouterContainerNames(t *testing.T) {
	if RouterWebName != "tainer-router-web" {
		t.Errorf("RouterWebName: got %q", RouterWebName)
	}
	if RouterSSHName != "tainer-router-ssh" {
		t.Errorf("RouterSSHName: got %q", RouterSSHName)
	}
}

func TestLabelConstants(t *testing.T) {
	cases := map[string]string{
		LabelPod:           "tainer.pod",
		LabelRole:          "tainer.role",
		LabelSubnetOctet:   "tainer.subnet-octet",
		LabelManifestPath:  "tainer.manifest-path",
		LabelManifestHash:  "tainer.manifest-hash",
		LabelPublishPrefix: "tainer.published.",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("label constant mismatch: got %q, want %q", got, want)
		}
	}
}

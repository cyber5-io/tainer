package pod

import (
	"testing"

	"github.com/cyber5-io/tainer/pkg/tainer/manifest"
)

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
	cases := []struct {
		name, got, want string
	}{
		{"LabelPod", LabelPod, "tainer.pod"},
		{"LabelRole", LabelRole, "tainer.role"},
		{"LabelPodID", LabelPodID, "tainer.pod-id"},
		{"LabelManifestPath", LabelManifestPath, "tainer.manifest-path"},
		{"LabelManifestHash", LabelManifestHash, "tainer.manifest-hash"},
		{"LabelPublishPrefix", LabelPublishPrefix, "tainer.published."},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestPublishLabel(t *testing.T) {
	if got := PublishLabel("db"); got != "tainer.published.db" {
		t.Errorf("PublishLabel: got %q, want tainer.published.db", got)
	}
}

func TestExecRoleResolution(t *testing.T) {
	cases := []struct {
		typ      manifest.ProjectType
		argRole  string
		wantRole string
	}{
		{manifest.TypeWordPress, "", "app"},
		{manifest.TypeReact, "", "web"},
		{manifest.TypeWordPress, "web", "web"},
	}
	for _, c := range cases {
		got := ResolveExecRole(c.typ, c.argRole)
		if got != c.wantRole {
			t.Errorf("(%s, %q) → %q, want %q", c.typ, c.argRole, got, c.wantRole)
		}
	}
}

package manifest

import (
	"testing"
)

func TestParseV2WordPress(t *testing.T) {
	data := []byte(`version: 2
project:
  name: mysite
  type: wordpress
  domain: mysite.test
runtime:
  php: "8.2"
  database: mariadb
  php-limits:
    upload_max_filesize: "50M"
    memory_limit: "256M"
pod:
  size: small
ports:
  - role: web
    container: 80
    protocol: http
  - role: web
    container: 443
    protocol: http
`)
	m, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if m.Version != 2 {
		t.Errorf("expected version 2, got %d", m.Version)
	}
	if m.Project.Name != "mysite" {
		t.Errorf("expected project name 'mysite', got %q", m.Project.Name)
	}
	if m.Project.Type != TypeWordPress {
		t.Errorf("expected project type %q, got %q", TypeWordPress, m.Project.Type)
	}
	if m.Pod.Size != PodSizeSmall {
		t.Errorf("expected pod size %q, got %q", PodSizeSmall, m.Pod.Size)
	}
	if len(m.Ports) != 2 {
		t.Errorf("expected 2 ports, got %d", len(m.Ports))
	}
	if m.Ports[0].Role != "web" || m.Ports[0].Container != 80 || m.Ports[0].Protocol != PortHTTP {
		t.Errorf("expected web/80/http, got %s/%d/%q", m.Ports[0].Role, m.Ports[0].Container, m.Ports[0].Protocol)
	}
	if m.Runtime.PHPLimits.UploadMaxFilesize != "50M" {
		t.Errorf("expected upload_max_filesize '50M', got %q", m.Runtime.PHPLimits.UploadMaxFilesize)
	}
}

func TestParseV2CustomPodLimits(t *testing.T) {
	data := []byte(`version: 2
project:
  name: myapp
  type: nodejs
  domain: myapp.test
runtime:
  node: "18"
  database: none
pod:
  size: custom
  containers:
    web:
      memory: 200M
      cpu: 0.3
    app:
      memory: 1G
      cpu: 1
    db:
      memory: 500M
      cpu: 0.5
ports:
  - role: app
    container: 3000
    protocol: tcp
`)
	m, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if m.Pod.Size != PodSizeCustom {
		t.Errorf("expected pod size %q, got %q", PodSizeCustom, m.Pod.Size)
	}
	if len(m.Pod.Containers) != 3 {
		t.Fatalf("expected 3 containers, got %d", len(m.Pod.Containers))
	}
	if m.Pod.Containers["app"].Memory != "1G" || m.Pod.Containers["app"].CPU != 1.0 {
		t.Errorf("expected app {1G,1.0}, got %+v", m.Pod.Containers["app"])
	}
	if m.Pod.Containers["web"].CPU != 0.3 {
		t.Errorf("expected web cpu 0.3, got %v", m.Pod.Containers["web"].CPU)
	}
}

func TestParseV2ProtocolDefaultHTTP(t *testing.T) {
	data := []byte(`version: 2
project:
  name: web
  type: react
  domain: web.test
runtime:
  node: "18"
  database: none
pod:
  size: nano
ports:
  - role: web
    container: 5173
`)
	m, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if len(m.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(m.Ports))
	}
	if m.Ports[0].Protocol != PortHTTP {
		t.Errorf("expected default protocol %q, got %q", PortHTTP, m.Ports[0].Protocol)
	}
}

func TestParseV2InvalidPodSize(t *testing.T) {
	data := []byte(`version: 2
project:
  name: bad
  type: php
  domain: bad.test
runtime:
  php: "8.2"
  database: none
pod:
  size: invalid
`)
	_, err := ParseBytes(data)
	if err == nil {
		t.Fatal("expected error for invalid pod size, got nil")
	}
}

func TestPortEntryHostPortAccepted(t *testing.T) {
	data := []byte(`version: 2
project:
  name: myapp
  type: nodejs
  domain: myapp.test
runtime:
  node: "20"
  database: none
pod:
  size: small
ports:
  - role: metrics
    container: 9090
    protocol: tcp
    host_port: 45678
`)
	m, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if len(m.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(m.Ports))
	}
	if m.Ports[0].HostPort != 45678 {
		t.Errorf("expected HostPort 45678, got %d", m.Ports[0].HostPort)
	}
}

func TestPortEntryHostPortRejectedBelowBand(t *testing.T) {
	data := []byte(`version: 2
project:
  name: myapp
  type: nodejs
  domain: myapp.test
runtime:
  node: "20"
  database: none
pod:
  size: small
ports:
  - role: metrics
    container: 9090
    protocol: tcp
    host_port: 30121
`)
	_, err := ParseBytes(data)
	if err == nil {
		t.Fatal("expected error for host_port below band, got nil")
	}
}

func TestPortEntryHostPortRejectedAboveBand(t *testing.T) {
	data := []byte(`version: 2
project:
  name: myapp
  type: nodejs
  domain: myapp.test
runtime:
  node: "20"
  database: none
pod:
  size: small
ports:
  - role: metrics
    container: 9090
    protocol: tcp
    host_port: 50000
`)
	_, err := ParseBytes(data)
	if err == nil {
		t.Fatal("expected error for host_port above band, got nil")
	}
}

func TestPortEntryHostPortRejectedOnHTTP(t *testing.T) {
	data := []byte(`version: 2
project:
  name: myapp
  type: nodejs
  domain: myapp.test
runtime:
  node: "20"
  database: none
pod:
  size: small
ports:
  - role: web
    container: 3000
    protocol: http
    host_port: 45000
`)
	_, err := ParseBytes(data)
	if err == nil {
		t.Fatal("expected error for host_port on http protocol, got nil")
	}
}

func TestMigrateV1WordPress(t *testing.T) {
	v1 := `
version: 1
project:
  name: oldwp
  type: wordpress
  domain: oldwp.tainer.me
runtime:
  php: "8.2"
  database: mariadb
  limits:
    memory_limit: 256M
mounts:
  - foo
`
	m, err := ParseBytes([]byte(v1))
	if err != nil {
		t.Fatalf("ParseBytes (v1 migration): %v", err)
	}
	if m.Version != 2 {
		t.Errorf("Version after migrate: got %d, want 2", m.Version)
	}
	if m.Pod.Size != PodSizeSmall {
		t.Errorf("default Pod.Size after migrate: got %q, want small", m.Pod.Size)
	}
	if m.Runtime.PHPLimits.MemoryLimit != "256M" {
		t.Errorf("php-limits.memory_limit after migrate: got %q, want 256M", m.Runtime.PHPLimits.MemoryLimit)
	}
	if len(m.Mounts) != 1 || m.Mounts[0] != "foo" {
		t.Errorf("mounts not preserved across migration: %v", m.Mounts)
	}
}

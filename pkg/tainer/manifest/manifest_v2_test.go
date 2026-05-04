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
  - port: 80
    protocol: http
  - port: 443
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
	if m.Ports[0].Port != 80 || m.Ports[0].Protocol != ProtocolHTTP {
		t.Errorf("expected port 80/http, got %d/%q", m.Ports[0].Port, m.Ports[0].Protocol)
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
  memory: 2G
  cpus: 2
  containers:
    app:
      memory: 1G
      cpus: 1
ports:
  - port: 3000
    protocol: tcp
`)
	m, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if m.Pod.Size != PodSizeCustom {
		t.Errorf("expected pod size %q, got %q", PodSizeCustom, m.Pod.Size)
	}
	if m.Pod.Memory != "2G" {
		t.Errorf("expected memory '2G', got %q", m.Pod.Memory)
	}
	if m.Pod.CPUs != "2" {
		t.Errorf("expected cpus '2', got %q", m.Pod.CPUs)
	}
	if len(m.Pod.Containers) == 0 {
		t.Fatal("expected containers, got none")
	}
	if m.Pod.Containers["app"].Memory != "1G" {
		t.Errorf("expected app memory '1G', got %q", m.Pod.Containers["app"].Memory)
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
  - port: 5173
`)
	m, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if len(m.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(m.Ports))
	}
	if m.Ports[0].Protocol != ProtocolHTTP {
		t.Errorf("expected default protocol %q, got %q", ProtocolHTTP, m.Ports[0].Protocol)
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

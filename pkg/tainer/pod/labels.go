// Package pod owns tainer's pod abstraction: a logical group of
// containers sharing a network and a resource budget. This file
// defines the on-engine identifiers (labels, names) — the single
// source of truth that every other pod operation reads back from.
package pod

import "fmt"

// Container/network names. Generic role names (web, app, db, mail,
// cache, ...) describe what the container does; the *contents* differ
// per project type but the role label is stable.
const (
	NamePrefix    = "tainer-"
	RouterWebName = "tainer-router-web"
	RouterSSHName = "tainer-router-ssh"
)

// Engine label keys. Kept under the `tainer.*` namespace so it's clear
// these come from us. Discovery queries filter on LabelPod.
const (
	LabelPod          = "tainer.pod"
	LabelRole         = "tainer.role"
	LabelPodID        = "tainer.pod-id"
	LabelDomain       = "tainer.domain"
	LabelManifestPath = "tainer.manifest-path"
	LabelManifestHash = "tainer.manifest-hash"
	// LabelPublishPrefix is concatenated with the role to form the full
	// label key (e.g. "tainer.published.db" = "30070").
	LabelPublishPrefix = "tainer.published."
)

// PodCgroup is the shared cgroup path for a project's pod. Every container
// in the pod runs as a leaf under it; the parent carries the pod's aggregate
// memory/CPU budget (smart pods).
func PodCgroup(project string) string { return "tainer-" + project }

// Role values.
const (
	RoleWeb     = "web"
	RoleApp     = "app"
	RoleDB      = "db"
	RoleMail    = "mail"
	RoleCache   = "cache"
	RoleSearch  = "search"
	RoleXdebug  = "xdebug"
	RoleQueue   = "queue"
	RoleStorage = "storage"
	RoleCustom  = "custom"
)

// NetworkName returns the engine network name for the given pod.
func NetworkName(pod string) string {
	return NamePrefix + pod
}

// ContainerName returns the engine container name for a (pod, role).
func ContainerName(pod, role string) string {
	return fmt.Sprintf("%s%s-%s", NamePrefix, pod, role)
}

// PublishLabel returns the per-role published-port label key.
func PublishLabel(role string) string {
	return LabelPublishPrefix + role
}

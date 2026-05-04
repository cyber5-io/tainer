package identity

import "fmt"

// Detect returns the uid/gid to inject into containers.
// In local dev, reads from project directory owner.
func Detect(projectDir string) (uint32, uint32, error) {
	return DetectFromDir(projectDir)
}

// EnvFlags returns the --env flags to pass to container run commands.
func EnvFlags(uid, gid uint32) []string {
	return []string{
		"--env", fmt.Sprintf("TAINER_UID=%d", uid),
		"--env", fmt.Sprintf("TAINER_GID=%d", gid),
	}
}

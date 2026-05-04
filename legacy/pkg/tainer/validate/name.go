package validate

import (
	"fmt"
	"regexp"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func ProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("project name must be 63 characters or fewer (got %d)", len(name))
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("project name must contain only lowercase letters, numbers, and hyphens, and must start and end with a letter or number")
	}
	return nil
}

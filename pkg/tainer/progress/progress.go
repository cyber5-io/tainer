// Package progress emits best-effort structured progress events for long
// operations to the stack daemon (POST /cyberstack/progress). A missing
// daemon, an older daemon without the route, or any rejection is ignored:
// reporting must never slow or break the operation it describes.
package progress

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cyber5-io/tainer/pkg/tainer/engine"
)

const postTimeout = 50 * time.Millisecond

type document struct {
	Op      string `json:"op"`
	Pod     string `json:"pod"`
	Step    string `json:"step"`
	Percent int    `json:"percent"`
	Message string `json:"message"`
	Done    bool   `json:"done"`
}

// Reporter returns a function that reports one step of op on pod.
// percent is -1 when unknown.
func Reporter(ctx context.Context, op, pod string) func(step, message string, percent int, done bool) {
	return func(step, message string, percent int, done bool) {
		body, err := json.Marshal(document{op, pod, step, percent, message, done})
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(ctx, postTimeout)
		defer cancel()
		_ = engine.PostJSON(ctx, "/cyberstack/progress", body)
	}
}

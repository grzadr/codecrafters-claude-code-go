package tools

import (
	"context"
	"time"
)

// RunCommand exports the unexported runCommand function for testing.
func RunCommand(ctx context.Context, args []string, gracePeriod time.Duration) (string, error) {
	return runCommand(ctx, args, gracePeriod)
}

// BashFunc exports the unexported bashFunc function for testing, so tests can pass a short
// per-command timeout instead of waiting for the real one.
func BashFunc(ctx context.Context, command string, timeout time.Duration) (Result, error) {
	return bashFunc(ctx, bashArgs{Command: command}, timeout)
}

package tools

import (
	"context"
	"time"
)

// RunCommand exports the unexported runCommand function for testing.
func RunCommand(ctx context.Context, args []string, gracePeriod time.Duration) ([]byte, error) {
	return runCommand(ctx, args, gracePeriod)
}

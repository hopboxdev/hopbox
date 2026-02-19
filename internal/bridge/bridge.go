package bridge

import "context"

// Bridge is the interface implemented by all local↔remote bridges.
type Bridge interface {
	// Start brings up the bridge. Blocks until ctx is cancelled.
	Start(ctx context.Context) error
	// Stop immediately tears down the bridge.
	Stop()
	// Status returns a human-readable status string.
	Status() string
}

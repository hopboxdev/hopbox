package main

import "fmt"

// DownCmd tears down the tunnel (no-op in foreground mode).
type DownCmd struct{}

func (c *DownCmd) Run() error {
	fmt.Println("In foreground mode, use Ctrl-C to stop the tunnel.")
	return nil
}

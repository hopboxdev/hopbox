package main

import (
	"fmt"
	"log"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/events"
)

// newEventBus builds the reconcile wake-up bus from config. The default inproc
// bus has no dependencies; nats connects to a broker so wake-ups fan across
// nodes (e.g. a hub and reconciler on different hosts).
func newEventBus(cfg config.Config) (events.Bus, error) {
	switch cfg.EventsKind {
	case "", "inproc":
		return events.NewInProc(), nil
	case "nats":
		bus, err := events.Connect(cfg.NATSURL)
		if err != nil {
			return nil, fmt.Errorf("connect nats %s: %w", cfg.NATSURL, err)
		}
		log.Printf("hopboxd: reconcile wake-ups via NATS %s", cfg.NATSURL)
		return bus, nil
	default:
		return nil, fmt.Errorf("unknown --events %q (want inproc|nats)", cfg.EventsKind)
	}
}

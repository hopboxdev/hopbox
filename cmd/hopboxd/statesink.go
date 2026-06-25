package main

import (
	"context"
	"log"

	"github.com/hopboxdev/hopbox/internal/core/store"
)

// storeSink lets the agenthub flip agent_connected in the store when agents
// connect/disconnect, then pokes the reconciler so it converges immediately
// (the event half of the hybrid loop) instead of waiting for the next tick —
// which matters for reaping ephemeral boxes the instant their owner detaches.
type storeSink struct {
	store   store.Store
	tenant  string
	trigger func(workspaceID, tenant string) // reconciler wake-up; nil = rely on the tick
}

func (s storeSink) SetAgentConnected(ctx context.Context, workspaceID string, connected bool) {
	w, err := s.store.GetWorkspace(ctx, s.tenant, workspaceID)
	if err != nil {
		log.Printf("statesink: get %s: %v", workspaceID, err)
		return
	}
	w.AgentConnected = connected
	if err := s.store.UpdateWorkspace(ctx, w); err != nil {
		log.Printf("statesink: update %s: %v", workspaceID, err)
		return
	}
	if s.trigger != nil {
		s.trigger(workspaceID, s.tenant)
	}
}

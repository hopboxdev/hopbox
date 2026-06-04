package main

import (
	"context"
	"log"

	"github.com/mesadev/mesa/internal/core/store"
)

// storeSink lets the agenthub flip agent_connected in the store when agents
// connect/disconnect. The reconciler then converges phase on its next tick.
type storeSink struct {
	store  store.Store
	tenant string
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
	}
}

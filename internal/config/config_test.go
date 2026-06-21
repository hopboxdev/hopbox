package config_test

import (
	"testing"

	"github.com/hopboxdev/hopbox/internal/config"
)

func TestParseDefaults(t *testing.T) {
	c, err := config.Parse([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if c.APIAddr != ":7700" || c.AgentListen != ":7777" {
		t.Fatalf("defaults wrong: %+v", c)
	}
	if c.AgentAdvertise != "host.docker.internal:7777" {
		t.Fatalf("advertise default wrong: %q", c.AgentAdvertise)
	}
}

func TestParseOverrides(t *testing.T) {
	c, err := config.Parse([]string{
		"--api-addr", ":9000", "--db", "/tmp/x.db", "--agent-bin", "/b/agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.APIAddr != ":9000" || c.DBPath != "/tmp/x.db" || c.AgentBin != "/b/agent" {
		t.Fatalf("overrides not applied: %+v", c)
	}
}

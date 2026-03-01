package service

import (
	"context"
	"net"
	"testing"
)

func TestCheckTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	err = checkTCP(addr)
	if err != nil {
		t.Fatalf("expected healthy, got: %v", err)
	}

	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	err = checkTCP(addr)
	if err == nil {
		t.Fatal("expected error for closed port")
	}
}

func TestCheckExec(t *testing.T) {
	ctx := context.Background()

	err := checkExec(ctx, "true")
	if err != nil {
		t.Fatalf("expected healthy, got: %v", err)
	}

	err = checkExec(ctx, "false")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
}

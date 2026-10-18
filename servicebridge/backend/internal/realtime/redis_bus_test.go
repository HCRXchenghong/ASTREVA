package realtime

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRedisBusIntegration(t *testing.T) {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		t.Skip("REDIS_ADDR is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	channel := "customer-service:test:" + randomTestID()

	hub1 := NewHubWithNodeID("test-node-1")
	hub2 := NewHubWithNodeID("test-node-2")
	bus1, err := NewRedisBus(ctx, addr, channel, "test-node-1", logger, hub1)
	if err != nil {
		t.Fatalf("bus1: %v", err)
	}
	defer bus1.Close()
	bus2, err := NewRedisBus(ctx, addr, channel, "test-node-2", logger, hub2)
	if err != nil {
		t.Fatalf("bus2: %v", err)
	}
	defer bus2.Close()

	hub1.SetPublisher(bus1.Publish)
	hub2.SetPublisher(bus2.Publish)
	adminClient := &Client{
		Role:      RoleAdmin,
		send:      make(chan []byte, 1),
		logger:    logger,
		AccountID: "admin_super",
	}
	hub2.Register(adminClient)
	defer hub2.Unregister(adminClient)

	hub1.BroadcastAdmins(Event{Event: "redis.test", Data: map[string]any{"ok": true}})

	select {
	case payload := <-adminClient.send:
		if !strings.Contains(string(payload), "redis.test") {
			t.Fatalf("unexpected payload: %s", payload)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for redis event")
	}
}

func randomTestID() string {
	return time.Now().UTC().Format("20060102150405.000000000")
}

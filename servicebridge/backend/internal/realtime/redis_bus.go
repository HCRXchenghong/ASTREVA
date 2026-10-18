package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const DefaultRedisChannel = "customer-service:ws-events"

type RedisBus struct {
	client  *redis.Client
	pubsub  *redis.PubSub
	channel string
	nodeID  string
	cancel  context.CancelFunc
	logger  *slog.Logger
}

func NewRedisBus(ctx context.Context, addr, channel, nodeID string, logger *slog.Logger, hub *Hub) (*RedisBus, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, nil
	}
	if channel == "" {
		channel = DefaultRedisChannel
	}
	options, err := redisOptions(addr)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	pubsub := client.Subscribe(ctx, channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		_ = client.Close()
		return nil, err
	}
	busCtx, cancel := context.WithCancel(context.Background())
	bus := &RedisBus{
		client:  client,
		pubsub:  pubsub,
		channel: channel,
		nodeID:  nodeID,
		cancel:  cancel,
		logger:  logger,
	}
	if logger != nil {
		logger.Info("redis event bus subscribed", "channel", channel)
	}
	go bus.consume(busCtx, hub)
	return bus, nil
}

func (b *RedisBus) Publish(event WireEvent) {
	if b == nil || b.client == nil {
		return
	}
	if event.NodeID == "" {
		event.NodeID = b.nodeID
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.client.Publish(ctx, b.channel, payload).Err(); err != nil && b.logger != nil {
		b.logger.Warn("redis publish failed", "error", err)
	}
}

func (b *RedisBus) Close() error {
	if b == nil || b.client == nil {
		return nil
	}
	if b.cancel != nil {
		b.cancel()
	}
	if b.pubsub != nil {
		_ = b.pubsub.Close()
	}
	return b.client.Close()
}

func (b *RedisBus) consume(ctx context.Context, hub *Hub) {
	ch := b.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var event WireEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				if b.logger != nil {
					b.logger.Warn("invalid redis event payload", "error", err)
				}
				continue
			}
			hub.Deliver(event)
		}
	}
}

func redisOptions(addr string) (*redis.Options, error) {
	if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
		return redis.ParseURL(addr)
	}
	return &redis.Options{Addr: addr}, nil
}

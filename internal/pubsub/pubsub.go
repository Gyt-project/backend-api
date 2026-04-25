// Package pubsub provides a thin Redis Pub/Sub wrapper used by the Live API
// to fan-out events across multiple server instances.
//
// Init must be called with an already-connected *redis.Client (typically the
// same client used by the cache package).  All operations degrade gracefully
// when the client is nil — callers never need to check whether Redis is
// available.
package pubsub

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

// client is the shared Redis client. nil → no-op (fail-open).
var client *redis.Client

// Init sets the Redis client used for all Pub/Sub operations.
// Call this once at startup, passing cache.Client (they share the same conn).
func Init(c *redis.Client) {
	client = c
}

// Publish sends payload to channel. Returns nil if client is unset (fail-open).
func Publish(ctx context.Context, channel string, payload []byte) error {
	if client == nil {
		return nil
	}
	if err := client.Publish(ctx, channel, payload).Err(); err != nil {
		log.Printf("[pubsub:warn] PUBLISH %s: %v", channel, err)
		return err
	}
	return nil
}

// Subscribe opens a subscription to channel and waits for the server
// confirmation before returning.  The caller owns the returned *redis.PubSub
// and is responsible for calling Close() when done.
//
// Returns an error (and nil PubSub) when the client is not initialised so the
// caller can fall back to local-only fan-out.
func Subscribe(ctx context.Context, channel string) (*redis.PubSub, error) {
	if client == nil {
		return nil, fmt.Errorf("pubsub: redis client not initialised")
	}
	sub := client.Subscribe(ctx, channel)
	// Receive the first message to confirm the subscription was accepted.
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("pubsub: subscribe %s: %w", channel, err)
	}
	return sub, nil
}

package cache

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client is the global Redis client. It is nil until Init is called successfully.
var Client *redis.Client

// Init connects to Redis and pings it to confirm readiness.
// addr defaults to "localhost:6379" if empty.
// A failed connection is logged and the Client is left nil so all cache
// operations below degrade gracefully (fail-open).
func Init(addr, password string) error {
	if addr == "" {
		addr = "localhost:6379"
	}
	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		return err
	}
	Client = c
	return nil
}

// Get returns the cached bytes for key, or (nil, nil) on a cache miss.
// Redis errors are logged and suppressed so the caller always falls through
// to the real data source.
func Get(ctx context.Context, key string) ([]byte, error) {
	if Client == nil {
		return nil, nil
	}
	val, err := Client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		log.Printf("[cache:warn] GET %s: %v", key, err)
		return nil, nil
	}
	return val, nil
}

// Set stores value under key with the given TTL.
// Errors are logged and suppressed.
func Set(ctx context.Context, key string, value []byte, ttl time.Duration) {
	if Client == nil {
		return
	}
	if err := Client.Set(ctx, key, value, ttl).Err(); err != nil {
		log.Printf("[cache:warn] SET %s: %v", key, err)
	}
}

// Delete removes one or more exact keys from the cache.
// Errors are logged and suppressed.
func Delete(ctx context.Context, keys ...string) {
	if Client == nil || len(keys) == 0 {
		return
	}
	if err := Client.Del(ctx, keys...).Err(); err != nil {
		log.Printf("[cache:warn] DEL: %v", err)
	}
}

// InvalidatePattern removes all keys matching the given Redis glob pattern.
// It uses SCAN with a batch size of 100 to avoid blocking the server.
func InvalidatePattern(ctx context.Context, pattern string) {
	if Client == nil {
		return
	}
	var cursor uint64
	for {
		keys, next, err := Client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			log.Printf("[cache:warn] SCAN %s: %v", pattern, err)
			return
		}
		if len(keys) > 0 {
			if err := Client.Del(ctx, keys...).Err(); err != nil {
				log.Printf("[cache:warn] DEL (pattern=%s): %v", pattern, err)
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
}

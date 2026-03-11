package cache_lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	// retrySignal is published to subscribers when the winner fails, telling them
	// to race for the lock and retry the fetch themselves.
	retrySignal = "retry"
	// maxRetries is the maximum number of times to retry fetching the data.
	maxRetries = 3
)

var errRetry = errors.New("cache: winner failed, retrying")

type CacheOptions struct {
	SubscriptionTimeout time.Duration
	UnsubscribeAndClose bool
}

type Cache[Data any] interface {
	RememberBlocking(ctx context.Context, missFn MissFunc[Data], hitFn HitFunc[Data], key string, ttl time.Duration) (*Data, error)
}

type cache[Data any] struct {
	client  *redis.Client
	options *CacheOptions
}

type MissFunc[Data any] func(ctx context.Context) (*Data, error)
type HitFunc[Data any] func(ctx context.Context, data *Data)

func NewCache[Data any](client *redis.Client, options *CacheOptions) Cache[Data] {
	builtOptions := &CacheOptions{}
	if options != nil {
		builtOptions = options
	}

	return &cache[Data]{
		client:  client,
		options: builtOptions,
	}
}

func (c *cache[Data]) getCachedData(ctx context.Context, key string) *Data {
	cachedData, _ := c.client.Get(ctx, key).Result()

	if cachedData == "" {
		return nil
	}

	var marshaledData Data
	err := json.Unmarshal([]byte(cachedData), &marshaledData)
	if err != nil {
		return nil
	}

	return &marshaledData
}

// RememberBlocking does the following:
//  1. If the data is already in the cache, it calls the hit function and returns it.
//  2. If the data is not in the cache, it tries to populate the cache by the following steps:
//     2.1. Elects a winner by racing for the cache key.
//     2.2. The winner calls the miss function and populates the cache.
//     2.3. If the winner succeeds, it publishes the data to subscribers, and the subscribers call the hit function.
//     2.4. If the winner fails, it publishes the retry signal to subscribers, and the subscribers start from step 2.1
//     2.5. A new call during this time will come in as a subscriber
//  3. If there's an error in step 2, fallback to the miss function.
func (c *cache[Data]) RememberBlocking(ctx context.Context, missFn MissFunc[Data], hitFn HitFunc[Data], key string, ttl time.Duration) (*Data, error) {
	if cachedData := c.getCachedData(ctx, key); cachedData != nil {
		hitFn(ctx, cachedData)

		return cachedData, nil
	}
	data, err := c.tryPopulatingCache(ctx, missFn, hitFn, key, ttl, 0)
	if err != nil {
		log.Println("cache: falling back to miss function:", err)

		return missFn(ctx)
	}

	return data, nil
}

func (c *cache[Data]) tryPopulatingCache(ctx context.Context, missFn MissFunc[Data], hitFn HitFunc[Data], key string, ttl time.Duration, retries int) (*Data, error) {
	if retries >= maxRetries {
		return nil, errors.New("exceeded max retries to populate cache")
	}
	success, err := c.client.SetNX(ctx, key, "", ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("cache: SetNX failed: %w", err)
	}
	if !success {
		data, err := c.rememberWait(ctx, key)
		if errors.Is(err, errRetry) {
			return c.tryPopulatingCache(ctx, missFn, hitFn, key, ttl, retries+1)
		}
		if err != nil {
			return nil, err
		}
		hitFn(ctx, data)

		return data, nil
	}
	data, err := missFn(ctx)
	if err != nil {
		c.client.Del(ctx, key)
		c.client.Publish(ctx, key, retrySignal)

		return nil, err
	}
	bytedata, _ := json.Marshal(*data)
	if _, err = c.client.Set(ctx, key, string(bytedata), ttl).Result(); err != nil {
		log.Println("cache: Set failed, releasing lock and notifying waiters:", err)
		c.client.Del(ctx, key)
		c.client.Publish(ctx, key, string(bytedata))

		return data, nil
	}
	if _, err = c.client.Publish(ctx, key, string(bytedata)).Result(); err != nil {
		log.Println("cache: Publish failed:", err)
	}

	return data, nil
}

func (c *cache[Data]) rememberWait(ctx context.Context, key string) (*Data, error) {
	subscription := NewCacheSubscription(c.client, key)
	subscription.Subscribe(ctx)

	defer func() {
		if c.options.UnsubscribeAndClose {
			subscription.UnsubscribeAndClose(ctx)

			return
		}
		err := subscription.Unsubscribe(ctx)
		if err != nil {
			log.Println(err)
		}
	}()

	channel, err := subscription.GetChannel(ctx)
	if err != nil {
		return nil, err
	}
	if c.options.SubscriptionTimeout != 0*time.Second {
		go func() {
			time.Sleep(c.options.SubscriptionTimeout)
			subscription.UnsubscribeAndClose(ctx)
		}()
	}

	for msg := range channel {
		if msg.Payload == retrySignal {
			return nil, errRetry
		}
		if msg.Payload != "" {
			var u Data
			// Unmarshal the data into the user
			if err := json.Unmarshal([]byte(msg.Payload), &u); err != nil {
				return nil, err
			}

			return &u, nil
		}
	}

	return nil, errors.New("error reading from cache subscription channel")
}

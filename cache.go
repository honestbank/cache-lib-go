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

const errCache = "cache:error"

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

func (c *cache[Data]) getCachedData(ctx context.Context, key string) (*Data, error) {
	cachedData, err := c.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil // legitimate cache miss
	}

	if err != nil {
		return nil, err // actual Redis error
	}

	if cachedData == "" {
		return nil, nil
	}

	var marshaledData Data
	if err := json.Unmarshal([]byte(cachedData), &marshaledData); err != nil {
		log.Printf("getCachedData: invalid JSON in cache, treating as miss: %v", err)

		return nil, nil
	}

	return &marshaledData, nil
}

func (c *cache[Data]) RememberBlocking(ctx context.Context, missFn MissFunc[Data], hitFn HitFunc[Data], key string, ttl time.Duration) (*Data, error) {
	cachedData, err := c.getCachedData(ctx, key)
	if err != nil {
		return nil, err
	}
	if cachedData != nil {
		hitFn(ctx, cachedData)

		return cachedData, nil
	}

	success, err := c.client.SetNX(ctx, key+":lock", "", ttl).Result()
	if err != nil {
		log.Println(err)

		return nil, err
	}

	if !success {
		return c.rememberWait(ctx, key)
	}

	defer c.client.Del(ctx, key+":lock")

	data, err := missFn(ctx)
	if err != nil {
		c.client.Publish(ctx, key, errCache)

		return nil, err
	}

	bytedata, err := json.Marshal(*data)
	if err != nil {
		log.Println(err)
		c.client.Publish(ctx, key, errCache)

		return nil, err
	}

	_, err = c.client.Set(ctx, key, string(bytedata), ttl).Result()
	if err != nil {
		log.Println(err)
		c.client.Publish(ctx, key, errCache)

		return nil, err
	}

	_, err = c.client.Publish(ctx, key, string(bytedata)).Result()
	if err != nil {
		log.Println(err)

		return nil, err
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
		if msg.Payload == "" {
			continue
		}
		// Check if the lock holder signalled a failure
		if msg.Payload == errCache {
			return nil, errors.New("cache fetch failed, upstream returned an error")
		}
		var u Data
		if err := json.Unmarshal([]byte(msg.Payload), &u); err != nil {
			return nil, fmt.Errorf("failed to unmarshal cache result: %w", err)
		}

		return &u, nil
	}

	return nil, errors.New("timed out waiting for cache result")
}

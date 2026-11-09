package cache_lib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"golang.org/x/sync/singleflight"
)

const (
	// retrySignal is published to subscribers when the winner fails, telling them
	// to race for the lock and retry the fetch themselves.
	retrySignal = "retry"
	// maxRetries is the maximum number of times to retry fetching the data.
	maxRetries = 3
)

var errRetry = errors.New("cache: winner failed, retrying")

// waits collapses the concurrent waits for the same Redis client and key in
// this process. Each wait holds one Pub/Sub connection, which go-redis v8
// dials outside the pool, so one process holds at most one Pub/Sub
// connection per key. It is a package variable because callers can make a
// new Cache for each request.
var waits singleflight.Group

type CacheOptions struct {
	// SubscriptionTimeout is the maximum time that a waiter waits for the
	// winner to publish the data. After it, the waiter falls back to the miss
	// function. Zero means no timeout: the wait then stops only when the
	// context of the first waiter is done.
	SubscriptionTimeout time.Duration
	// UnsubscribeAndClose is kept for backward compatibility and has no
	// effect: RememberBlocking always unsubscribes and closes its
	// subscription.
	UnsubscribeAndClose bool
}

type Cache[Data any] interface {
	RememberBlocking(ctx context.Context, missFn MissFunc[Data], hitFn HitFunc[Data], key string, ttl time.Duration) (*Data, error)
}

type cache[Data any] struct {
	client          *redis.Client
	options         *CacheOptions
	newSubscription func(client *redis.Client, channel string) CacheSubscription
}

type MissFunc[Data any] func(ctx context.Context) (*Data, error)
type HitFunc[Data any] func(ctx context.Context, data *Data)

func NewCache[Data any](client *redis.Client, options *CacheOptions) Cache[Data] {
	builtOptions := &CacheOptions{}
	if options != nil {
		builtOptions = options
	}

	return &cache[Data]{
		client:          client,
		options:         builtOptions,
		newSubscription: NewCacheSubscription,
	}
}

func (c *cache[Data]) getCachedData(ctx context.Context, key string) *Data {
	cachedData, err := c.client.Get(ctx, key).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			log.Println("cache: Get failed:", err)
		}

		return nil
	}
	if cachedData == "" {
		return nil
	}

	var marshaledData Data
	err = json.Unmarshal([]byte(cachedData), &marshaledData)
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
	bytedata, err := json.Marshal(*data)
	if err != nil {
		log.Println("cache: marshal failed, releasing lock and notifying waiters:", err)
		c.client.Del(ctx, key)
		c.client.Publish(ctx, key, retrySignal)

		return data, nil
	}
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

// rememberWait waits for the winner to publish the data of key. Concurrent
// waits for the same client, timeout and key share one subscription. The
// timeout is part of the key, so each caller gets its own timeout.
func (c *cache[Data]) rememberWait(ctx context.Context, key string) (*Data, error) {
	waitKey := fmt.Sprintf("%p\x00%d\x00%s", c.client, c.options.SubscriptionTimeout, key)
	result, err, _ := waits.Do(waitKey, func() (any, error) {
		return c.waitForPayload(ctx, key)
	})
	if err != nil {
		return nil, err
	}
	payload, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("cache: unexpected wait result of type %T", result)
	}
	if payload == retrySignal {
		return nil, errRetry
	}

	var data Data
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return nil, err
	}

	return &data, nil
}

// waitForPayload subscribes to key and returns the first payload that is not
// empty. It always unsubscribes and closes the subscription before it
// returns.
func (c *cache[Data]) waitForPayload(ctx context.Context, key string) (string, error) {
	waitCtx := ctx
	if c.options.SubscriptionTimeout > 0 {
		// The other waiters of this key share the wait, so a cancelled first
		// waiter must not stop it. The timeout limits the wait.
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), c.options.SubscriptionTimeout)
		defer cancel()
	}

	subscription := c.newSubscription(c.client, key).Subscribe(waitCtx)
	defer func() {
		if err := subscription.UnsubscribeAndClose(waitCtx); err != nil {
			log.Println("cache: subscription cleanup failed:", err)
		}
	}()

	channel, err := subscription.GetChannel(waitCtx)
	if err != nil {
		return "", err
	}

	for {
		select {
		case <-waitCtx.Done():
			return "", fmt.Errorf("cache: waiting for the cache subscription: %w", waitCtx.Err())
		case msg, ok := <-channel:
			if !ok {
				return "", errors.New("error reading from cache subscription channel")
			}
			if msg.Payload != "" {
				return msg.Payload, nil
			}
		}
	}
}

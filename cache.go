package cache_lib

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
)

type Cache[Data any] interface {
	RememberBlocking(ctx context.Context, missFn MissFunc[Data], hitFn HitFunc[Data], key string, ttl time.Duration) (*Data, error)
}

type cache[Data any] struct {
	client *redis.Client
}

type MissFunc[Data any] func(ctx context.Context) (*Data, error)
type HitFunc[Data any] func(ctx context.Context, data *Data)

func NewCache[Data any](client *redis.Client) Cache[Data] {
	return &cache[Data]{
		client: client,
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

func (c *cache[Data]) RememberBlocking(ctx context.Context, missFn MissFunc[Data], hitFn HitFunc[Data], key string, ttl time.Duration) (*Data, error) {
	cachedData := c.getCachedData(ctx, key)
	if cachedData != nil {
		hitFn(ctx, cachedData)

		return cachedData, nil
	}
	success, err := c.client.SetNX(ctx, key, "", ttl).Result()
	if err != nil {
		log.Println(err)

		return nil, err
	}
	if !success {
		return c.rememberWait(ctx, key)
	}
	data, err := missFn(ctx)
	if err != nil {
		c.client.Publish(ctx, key, "cache miss")

		return nil, err
	}
	bytedata, err := json.Marshal(*data)
	if err != nil {
		return nil, err
	}
	_, err = c.client.Set(ctx, key, string(bytedata), ttl).Result()
	if err != nil {
		log.Println(err)

		return nil, err
	}
	_, err = c.client.Publish(ctx, key, string(bytedata)).Result()
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (c *cache[Data]) rememberWait(ctx context.Context, key string) (*Data, error) {
	subscription := NewCacheSubscription(c.client, key)
	subscription.Subscribe(ctx)
	defer func() {
		err := subscription.Unsubscribe(ctx)
		if err != nil {
			log.Println(err)
		}
	}()

	channel, err := subscription.GetChannel(ctx)
	if err != nil {
		return nil, err
	}

	for msg := range channel {
		if msg.Payload != "" {
			var u Data
			// Unmarshal the data into the user
			if err := json.Unmarshal([]byte(msg.Payload), &u); err != nil {
				return nil, err
			}

			return &u, nil
		}
	}

	return nil, errors.New("error reading from pub/sub")
}

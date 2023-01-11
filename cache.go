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
	RememberBlocking(ctx context.Context, fn LongFunc[Data], key string, ttl time.Duration) (*Data, error)
}

type cache[Data any] struct {
	client *redis.Client
}

type LongFunc[Data any] func(ctx context.Context) (*Data, error)

func NewCache[Data any](client *redis.Client) Cache[Data] {
	return &cache[Data]{
		client: client,
	}
}

func (c *cache[Data]) RememberBlocking(ctx context.Context, fn LongFunc[Data], key string, ttl time.Duration) (*Data, error) {
	success, err := c.client.SetNX(ctx, key, "", ttl).Result()
	if err != nil {
		log.Println(err)

		return nil, err
	}
	if !success {
		return c.rememberWait(ctx, key)
	}
	data, err := fn(ctx)
	if err != nil {
		c.client.Publish(ctx, key, "cache miss")

		return nil, err
	}
	bytedata, err := json.Marshal(*data)
	if err != nil {
		return nil, err
	}

	_, err = c.client.Publish(ctx, key, string(bytedata)).Result()
	if err != nil {
		return nil, err
	}

	c.client.Del(ctx, key)

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

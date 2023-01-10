package cache_lib

import (
	"context"
	"errors"

	"github.com/go-redis/redis/v8"
)

type CacheSubscription interface {
	Unsubscribe(ctx context.Context) error
	Subscribe(ctx context.Context) CacheSubscription
	GetChannel(ctx context.Context) (<-chan *redis.Message, error)
}

type cacheSubscription struct {
	channel      string
	client       *redis.Client
	Subscription *redis.PubSub
}

func NewCacheSubscription(client *redis.Client, channel string) CacheSubscription {
	return &cacheSubscription{
		client:       client,
		channel:      channel,
		Subscription: nil,
	}
}

func (cs *cacheSubscription) Unsubscribe(ctx context.Context) error {
	if cs.Subscription == nil {
		return nil
	}
	err := cs.Subscription.Unsubscribe(ctx, cs.channel)
	if err != nil {
		return err
	}

	cs.Subscription = nil

	return nil
}

func (cs *cacheSubscription) Subscribe(ctx context.Context) CacheSubscription {
	cs.Subscription = cs.client.Subscribe(ctx, cs.channel)
	if cs.Subscription == nil {
		panic("no subscription created")
	}

	return cs
}

func (cs *cacheSubscription) GetChannel(ctx context.Context) (<-chan *redis.Message, error) {
	if cs.Subscription == nil {
		return nil, errors.New("No subscription")
	}

	return cs.Subscription.Channel(), nil
}

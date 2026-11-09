package cache_lib

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// cleanupTimeout limits the time that the UNSUBSCRIBE command can take. The
// cleanup does not use the deadline or the cancellation of the request
// context, because a cancelled request must still release its connection.
const cleanupTimeout = time.Second

type CacheSubscription interface {
	Unsubscribe(ctx context.Context) error
	UnsubscribeAndClose(ctx context.Context) error
	Subscribe(ctx context.Context) CacheSubscription
	GetChannel(ctx context.Context) (<-chan *redis.Message, error)
}

type cacheSubscription struct {
	channel string
	client  *redis.Client

	// mu protects Subscription. The cleanup can run from more than one
	// goroutine, and only one of them must close the Pub/Sub connection.
	mu           sync.Mutex
	Subscription *redis.PubSub
}

func NewCacheSubscription(client *redis.Client, channel string) CacheSubscription {
	return &cacheSubscription{
		client:       client,
		channel:      channel,
		Subscription: nil,
	}
}

// Unsubscribe unsubscribes from the channel and closes the Pub/Sub
// connection. It is the same as UnsubscribeAndClose: a go-redis v8 PubSub
// dials its own connection outside the pool, and a PubSub that is not closed
// keeps that connection (or dials it again) for the life of the process.
func (cs *cacheSubscription) Unsubscribe(ctx context.Context) error {
	return cs.UnsubscribeAndClose(ctx)
}

// UnsubscribeAndClose unsubscribes from the channel and always closes the
// Pub/Sub connection, also when the UNSUBSCRIBE command fails. It is safe to
// call more than once and from more than one goroutine: only the first call
// does the cleanup, the other calls return nil.
func (cs *cacheSubscription) UnsubscribeAndClose(ctx context.Context) error {
	cs.mu.Lock()
	pubSub := cs.Subscription
	cs.Subscription = nil
	cs.mu.Unlock()

	return closePubSub(ctx, pubSub, cs.channel)
}

func closePubSub(ctx context.Context, pubSub *redis.PubSub, channel string) error {
	if pubSub == nil {
		return nil
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	var unsubscribeErr, closeErr error
	if err := pubSub.Unsubscribe(cleanupCtx, channel); err != nil {
		unsubscribeErr = fmt.Errorf("cache: unsubscribe failed: %w", err)
	}
	if err := pubSub.Close(); err != nil && !errors.Is(err, redis.ErrClosed) {
		closeErr = fmt.Errorf("cache: closing the subscription failed: %w", err)
	}

	return errors.Join(unsubscribeErr, closeErr)
}

func (cs *cacheSubscription) Subscribe(ctx context.Context) CacheSubscription {
	pubSub := cs.client.Subscribe(ctx, cs.channel)
	if pubSub == nil {
		panic("no subscription created")
	}

	cs.mu.Lock()
	previous := cs.Subscription
	cs.Subscription = pubSub
	cs.mu.Unlock()

	if err := closePubSub(ctx, previous, cs.channel); err != nil {
		log.Println("cache: closing the previous subscription failed:", err)
	}

	return cs
}

func (cs *cacheSubscription) GetChannel(ctx context.Context) (<-chan *redis.Message, error) {
	cs.mu.Lock()
	pubSub := cs.Subscription
	cs.mu.Unlock()

	if pubSub == nil {
		return nil, errors.New("no subscription")
	}

	return pubSub.Channel(), nil
}

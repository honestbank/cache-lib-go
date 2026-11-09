package cache_lib

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDialRefused = errors.New("dial refused")

// newUnreachableClient returns a client whose connections always fail, as when
// Redis resets the connection or refuses it at its connection limit. It needs
// no network.
func newUnreachableClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: "redis.invalid:6379",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errDialRefused
		},
		MaxRetries: -1,
	})
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func TestUnsubscribeAndClose_ClosesWhenUnsubscribeFails(t *testing.T) {
	for name, cleanup := range map[string]func(context.Context, CacheSubscription) error{
		"UnsubscribeAndClose": func(ctx context.Context, s CacheSubscription) error { return s.UnsubscribeAndClose(ctx) },
		"Unsubscribe":         func(ctx context.Context, s CacheSubscription) error { return s.Unsubscribe(ctx) },
	} {
		t.Run(name, func(t *testing.T) {
			subscription, ok := NewCacheSubscription(newUnreachableClient(t), "key").Subscribe(context.Background()).(*cacheSubscription)
			require.True(t, ok)
			pubSub := subscription.Subscription
			require.NotNil(t, pubSub)

			// Channel starts the go-redis goroutine that redials forever until
			// the PubSub is closed.
			channel, err := subscription.GetChannel(context.Background())
			require.NoError(t, err)

			// A cancelled request context must not stop the cleanup.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err = cleanup(ctx, subscription)
			require.ErrorIs(t, err, errDialRefused)
			assert.Nil(t, subscription.Subscription)
			assert.ErrorIs(t, pubSub.Ping(context.Background()), redis.ErrClosed, "the PubSub must be closed")

			select {
			case _, open := <-channel:
				assert.False(t, open, "the channel must be closed")
			case <-time.After(5 * time.Second):
				t.Fatal("the go-redis receive goroutine did not stop")
			}

			assert.NoError(t, subscription.UnsubscribeAndClose(context.Background()), "a second call does nothing")
		})
	}
}

func TestUnsubscribeAndClose_ConcurrentCalls(t *testing.T) {
	subscription, ok := NewCacheSubscription(newUnreachableClient(t), "key").Subscribe(context.Background()).(*cacheSubscription)
	require.True(t, ok)
	pubSub := subscription.Subscription
	_, err := subscription.GetChannel(context.Background())
	require.NoError(t, err)

	const callers = 16
	var (
		wg     sync.WaitGroup
		failed atomic.Int32
	)
	for i := range callers {
		wg.Go(func() {
			var err error
			if i%2 == 0 {
				err = subscription.UnsubscribeAndClose(context.Background())
			} else {
				err = subscription.Unsubscribe(context.Background())
			}
			if err != nil {
				failed.Add(1)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, int32(1), failed.Load(), "only one call does the cleanup")
	assert.Nil(t, subscription.Subscription)
	assert.ErrorIs(t, pubSub.Ping(context.Background()), redis.ErrClosed)
}

// fakeSubscription is a CacheSubscription that does not use Redis.
type fakeSubscription struct {
	channel chan *redis.Message
	closes  atomic.Int32
}

func (f *fakeSubscription) Unsubscribe(ctx context.Context) error {
	return f.UnsubscribeAndClose(ctx)
}

func (f *fakeSubscription) UnsubscribeAndClose(context.Context) error {
	f.closes.Add(1)

	return nil
}

func (f *fakeSubscription) Subscribe(context.Context) CacheSubscription { return f }

func (f *fakeSubscription) GetChannel(context.Context) (<-chan *redis.Message, error) {
	return f.channel, nil
}

func newFakeCache(t *testing.T, timeout time.Duration) (*cache[testData], *fakeSubscription, *atomic.Int32) {
	t.Helper()
	fake := &fakeSubscription{channel: make(chan *redis.Message, 1)}
	subscriptions := &atomic.Int32{}
	c := &cache[testData]{
		// The fake does not use the client. The cache uses its address only
		// in the key of the shared wait.
		client:  &redis.Client{},
		options: &CacheOptions{SubscriptionTimeout: timeout},
		newSubscription: func(*redis.Client, string) CacheSubscription {
			subscriptions.Add(1)

			return fake
		},
	}

	return c, fake, subscriptions
}

type testData struct {
	Value string `json:"value"`
}

func TestRememberWait_CollapsesConcurrentWaits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, fake, subscriptions := newFakeCache(t, 5*time.Second)

		const waiters = 10
		results := make([]*testData, waiters)
		errs := make([]error, waiters)
		var wg sync.WaitGroup
		for i := range waiters {
			wg.Go(func() {
				results[i], errs[i] = c.rememberWait(context.Background(), "collapse-key")
			})
		}

		// Wait until all waiters block: one in the subscription, the others
		// in the shared call.
		synctest.Wait()
		fake.channel <- &redis.Message{Payload: `{"value":"v"}`}
		wg.Wait()

		assert.Equal(t, int32(1), subscriptions.Load(), "one subscription for all waiters")
		assert.Equal(t, int32(1), fake.closes.Load(), "the subscription is closed once")
		for i := range waiters {
			require.NoError(t, errs[i])
			assert.Equal(t, &testData{Value: "v"}, results[i])
		}
	})
}

func TestRememberWait_DifferentTimeoutsDoNotShareAWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		short, _, shortSubscriptions := newFakeCache(t, time.Second)
		long, _, longSubscriptions := newFakeCache(t, 3*time.Second)
		long.client = short.client

		start := time.Now()
		var shortElapsed time.Duration
		var shortErr error
		var wg sync.WaitGroup
		wg.Go(func() {
			_, shortErr = short.rememberWait(context.Background(), "timeout-mix-key")
			shortElapsed = time.Since(start)
		})
		// The short wait starts first, so a shared wait would use its timeout.
		synctest.Wait()

		_, longErr := long.rememberWait(context.Background(), "timeout-mix-key")
		longElapsed := time.Since(start)
		wg.Wait()

		require.ErrorIs(t, shortErr, context.DeadlineExceeded)
		require.ErrorIs(t, longErr, context.DeadlineExceeded)
		assert.Equal(t, time.Second, shortElapsed)
		assert.Equal(t, 3*time.Second, longElapsed)
		assert.Equal(t, int32(1), shortSubscriptions.Load())
		assert.Equal(t, int32(1), longSubscriptions.Load())
	})
}

func TestRememberWait_TimeoutClosesSubscription(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, fake, _ := newFakeCache(t, time.Second)

		start := time.Now()
		data, err := c.rememberWait(context.Background(), "timeout-key")

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, data)
		assert.Equal(t, time.Second, time.Since(start))
		assert.Equal(t, int32(1), fake.closes.Load())
	})
}

func TestRememberWait_RetrySignal(t *testing.T) {
	c, fake, _ := newFakeCache(t, time.Second)
	fake.channel <- &redis.Message{Payload: retrySignal}

	_, err := c.rememberWait(context.Background(), "retry-key")

	require.ErrorIs(t, err, errRetry)
	assert.Equal(t, int32(1), fake.closes.Load())
}

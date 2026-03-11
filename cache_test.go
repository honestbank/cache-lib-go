package cache_lib_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redismock/v8"
	"github.com/stretchr/testify/assert"

	cache_lib "github.com/honestbank/cache-lib-go"
)

type Response struct {
	Result bool `json:"result"`
}

func TestNewCache(t *testing.T) {
	a := assert.New(t)

	db, _ := redismock.NewClientMock()
	cache := cache_lib.NewCache[Response](db, nil)

	a.NotNil(cache)
}

func TestCache_RememberBlocking(t *testing.T) {
	t.Run("happy path - cache miss then fetch", func(t *testing.T) {
		a := assert.New(t)
		db, mock := redismock.NewClientMock()
		cache := cache_lib.NewCache[Response](db, nil)

		response := Response{Result: true}
		responseString, _ := json.Marshal(response)

		mock.ExpectGet("data").SetVal("")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectSet("data", string(responseString), 1*time.Second).SetVal(string(responseString))
		mock.ExpectPublish("data", string(responseString)).SetVal(1)

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(response, *result)
	})

	t.Run("setNX error - falls back to missFn", func(t *testing.T) {
		a := assert.New(t)
		db, mock := redismock.NewClientMock()
		cache := cache_lib.NewCache[Response](db, nil)

		response := Response{Result: true}

		mock.ExpectGet("data").SetVal("")
		mock.ExpectSetNX("data", "", 1*time.Second).SetErr(errors.New("redis unavailable"))

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(response, *result)
	})

	t.Run("set error after missFn - releases lock, notifies waiters, returns data", func(t *testing.T) {
		a := assert.New(t)
		db, mock := redismock.NewClientMock()
		cache := cache_lib.NewCache[Response](db, nil)

		response := Response{Result: true}
		responseString, _ := json.Marshal(response)

		mock.ExpectGet("data").SetVal("")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectSet("data", string(responseString), 1*time.Second).SetErr(errors.New("redis unavailable"))
		mock.ExpectDel("data").SetVal(1)
		mock.ExpectPublish("data", string(responseString)).SetVal(1)

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(response, *result)
		a.NoError(mock.ExpectationsWereMet())
	})

	t.Run("publish error after missFn - returns data without error", func(t *testing.T) {
		a := assert.New(t)
		db, mock := redismock.NewClientMock()
		cache := cache_lib.NewCache[Response](db, nil)

		response := Response{Result: true}
		responseString, _ := json.Marshal(response)

		mock.ExpectGet("data").SetVal("")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectSet("data", string(responseString), 1*time.Second).SetVal("OK")
		mock.ExpectPublish("data", string(responseString)).SetErr(errors.New("redis unavailable"))

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(response, *result)
	})

	t.Run("missFn error - releases lock, signals subscribers to retry, propagates error", func(t *testing.T) {
		a := assert.New(t)
		db, mock := redismock.NewClientMock()
		cache := cache_lib.NewCache[Response](db, nil)

		mock.ExpectGet("data").SetVal("")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectDel("data").SetVal(1)
		mock.ExpectPublish("data", "retry").SetVal(0)

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			return nil, errors.New("upstream failed")
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.Error(err)
		a.Nil(result)
		a.NoError(mock.ExpectationsWereMet())
	})
}

func TestNewCacheSubscription(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: ":6379", // We connect to host redis, thats what the hostname of the redis service is set to in the docker-compose
		DB:   0,
	})

	cache := cache_lib.NewCache[Response](redisClient, nil)

	t.Run("Multiple calls", func(t *testing.T) {
		a := assert.New(t)

		response := Response{Result: true}

		go func() {
			_, _ = cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
				time.Sleep(2 * time.Second)

				return &response, nil
			}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)
		}()

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(response, *result)
	})
}

func TestNewCacheSubscriptionWithOptions(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: ":6379", // We connect to host redis, thats what the hostname of the redis service is set to in the docker-compose
		DB:   0,
	})

	cache := cache_lib.NewCache[Response](redisClient, &cache_lib.CacheOptions{
		SubscriptionTimeout: 1 * time.Second,
		UnsubscribeAndClose: true,
	})

	t.Run("Multiple calls", func(t *testing.T) {
		a := assert.New(t)

		response := Response{Result: true}

		go func() {
			_, _ = cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
				time.Sleep(2 * time.Second)

				return &response, nil
			}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)
		}()

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(response, *result)
	})
}

func TestRememberWait(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: ":6379", // We connect to host redis, thats what the hostname of the redis service is set to in the docker-compose
		DB:   0,
	})

	cache := cache_lib.NewCache[Response](redisClient, &cache_lib.CacheOptions{
		SubscriptionTimeout: 3 * time.Second,
		UnsubscribeAndClose: true,
	})

	t.Run("single cache", func(t *testing.T) {
		a := assert.New(t)
		response := Response{Result: true}
		//responseString, _ := json.Marshal(response)
		go func() {
			_, _ = cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
				time.Sleep(2 * time.Second)

				return &response, nil
			}, func(ctx context.Context, data *Response) {}, "data3", 1*time.Second)
		}()
		_, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data3", 1*time.Second)

		a.NoError(err)
	})
}

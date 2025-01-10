package cache_lib_test

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
	db, mock := redismock.NewClientMock()
	cache := cache_lib.NewCache[Response](db, nil)
	t.Run("single cache", func(t *testing.T) {
		a := assert.New(t)

		response := Response{Result: true}
		responseString, _ := json.Marshal(response)

		mock.ExpectGet("data").SetVal("")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectSet("data", string(responseString), 1*time.Second).SetVal(string(responseString))
		mock.ExpectPublish("data", string(responseString)).SetVal(1)

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(response, *result)
	})
	t.Run("single cache - invalid cached", func(t *testing.T) {
		a := assert.New(t)

		response := Response{Result: true}
		responseString, _ := json.Marshal(response)

		mock.ExpectGet("data").SetVal("not valid json")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectSet("data", string(responseString), 1*time.Second).SetVal(string(responseString))
		mock.ExpectPublish("data", string(responseString)).SetVal(1)

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(response, *result)
	})
	t.Run("single cache - setNX error", func(t *testing.T) {
		a := assert.New(t)

		response := Response{Result: true}

		mock.ExpectSetNX("data", "", 1*time.Second).SetErr(errors.New("Unable to set"))

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.Error(err)
		a.Nil(result)
	})

	t.Run("single cache - request error", func(t *testing.T) {
		a := assert.New(t)

		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return nil, errors.New("request Failed")
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.Error(err)
		a.Nil(result)
	})

	t.Run("single cache", func(t *testing.T) {
		a := assert.New(t)

		response := Response{Result: true}
		responseString, _ := json.Marshal(response)

		mock.ExpectGet("data").SetVal("{}")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectSetNX("data", string(responseString), 1*time.Second).SetVal(true)

		mock.ExpectPublish("data", string(responseString)).SetVal(1)

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.NoError(err)
		a.Equal(Response{}, *result)
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

func TestConcurrentRequestsForSameKey(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: ":6379",
		DB:   0,
	})

	subscriptionTimeout := time.Second * 5
	keyTTL := time.Second * 2
	requestProcessingTime := 100 * time.Millisecond

	response := Response{Result: true}

	// First request
	resourceKey := "data"
	go func() {
		cache := cache_lib.NewCache[Response](redisClient, &cache_lib.CacheOptions{
			SubscriptionTimeout: subscriptionTimeout,
			UnsubscribeAndClose: true,
		})
		data, err := cache.RememberBlocking(
			context.Background(),
			func(ctx context.Context) (*Response, error) {
				log.Println("miss from 1")
				time.Sleep(requestProcessingTime)
				return &response, nil
			},
			func(ctx context.Context, data *Response) {
				log.Println("hit from 1 request")
			},
			resourceKey,
			keyTTL,
		)
		log.Println("first request finished", data, err)
	}()

	// Second request come with some delay. Key is existing but subscription missed event and stuck till subscriptionTimeout
	time.Sleep(requestProcessingTime + time.Millisecond*1)

	cache := cache_lib.NewCache[Response](redisClient, &cache_lib.CacheOptions{
		SubscriptionTimeout: subscriptionTimeout,
		UnsubscribeAndClose: true,
	})
	result, err := cache.RememberBlocking(context.Background(),
		func(ctx context.Context) (*Response, error) {
			log.Println("miss from 2")
			time.Sleep(requestProcessingTime)
			return &response, nil
		},
		func(ctx context.Context, data *Response) {
			log.Println("hit from 2 request")
		},
		resourceKey,
		keyTTL)
	log.Println("second request finished", result, err)
	assert.NoError(t, err)
}

func TestPublishBeforeSubscribe(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: ":6379",
		DB:   0,
	})
	_, err := redisClient.Publish(context.Background(), "foo", "before subscribe").Result()
	assert.NoError(t, err)

	subscription := cache_lib.NewCacheSubscription(redisClient, "foo")
	subscription.Subscribe(context.Background())
	channel, err := subscription.GetChannel(context.Background())
	assert.NoError(t, err)
	// Subscription timeout
	go func() {
		time.Sleep(time.Second * 5)
		_ = subscription.UnsubscribeAndClose(context.Background())
	}()
	go func() {
		_, err := redisClient.Publish(context.Background(), "foo", "publish immediately").Result()
		assert.NoError(t, err)
	}()
	go func() {
		time.Sleep(time.Millisecond * 10)
		_, err := redisClient.Publish(context.Background(), "foo", "after subscribe").Result()
		assert.NoError(t, err)
	}()
	for msg := range channel {
		log.Println(msg.Payload)
	}

}
func TestCacheFailSet(t *testing.T) {
	db, mock := redismock.NewClientMock()

	cache := cache_lib.NewCache[Response](db, nil)

	t.Run("fail set", func(t *testing.T) {
		a := assert.New(t)

		response := Response{Result: true}

		mock.ExpectGet("data").SetVal("")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectSet("data", "", 1*time.Second).SetErr(errors.New("error"))

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.Error(err)
		a.Nil(result)
	})

	t.Run("fail publish", func(t *testing.T) {
		a := assert.New(t)

		response := Response{Result: true}

		mock.ExpectGet("data").SetVal("")
		mock.ExpectSetNX("data", "", 1*time.Second).SetVal(true)
		mock.ExpectSet("data", "{\"result\":true}", 1*time.Second).SetVal("OK")
		mock.ExpectPublish("data", response).SetErr(errors.New("error"))

		result, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(2 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data", 1*time.Second)

		a.Error(err)
		a.Nil(result)
	})
}

func TestRememberWait(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: ":6379", // We connect to host redis, thats what the hostname of the redis service is set to in the docker-compose
		DB:   0,
	})

	cache := cache_lib.NewCache[Response](redisClient, &cache_lib.CacheOptions{
		SubscriptionTimeout: 1 * time.Second,
		UnsubscribeAndClose: true,
	})

	t.Run("single cache", func(t *testing.T) {
		a := assert.New(t)
		response := Response{Result: true}
		//responseString, _ := json.Marshal(response)
		go func() {
			_, _ = cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
				time.Sleep(8 * time.Second)

				return &response, nil
			}, func(ctx context.Context, data *Response) {}, "data2", 1*time.Second)
		}()
		_, err := cache.RememberBlocking(context.Background(), func(ctx context.Context) (*Response, error) {
			time.Sleep(5 * time.Second)

			return &response, nil
		}, func(ctx context.Context, data *Response) {}, "data2", 1*time.Second)

		a.Error(err)
		a.Equal("error reading from pub/sub", err.Error())
	})
}

func TestRememberWait2(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr: ":6379", // We connect to host redis, thats what the hostname of the redis service is set to in the docker-compose
		DB:   0,
	})

	cache := cache_lib.NewCache[Response](redisClient, &cache_lib.CacheOptions{
		SubscriptionTimeout: 1 * time.Second,
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

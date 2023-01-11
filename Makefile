generate: mocks

mocks:
	go get github.com/golang/mock/mockgen/model
	go install github.com/golang/mock/mockgen@v1.6.0
	mockgen -destination=./mocks/mock_cache_subscription.go -package=mocks github.com/honestbank/cache-lib-go CacheSubscription

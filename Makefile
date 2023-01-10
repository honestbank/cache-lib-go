generate: mocks

mocks:
	go get github.com/golang/mock/mockgen/model
	go install github.com/golang/mock/mockgen@v1.6.0

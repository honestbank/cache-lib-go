package user_client

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/go-resty/resty/v2"
)

type userClient struct {
	RestyClient *resty.Client
}

type UserBody struct {
	Gender string `json:"gender"`
}

type UserClient interface {
	GetUser(ctx context.Context, body UserBody) (*io.ReadCloser, error)
}

func NewUserClient(client *http.Client) UserClient {
	restyClient := resty.NewWithClient(client)
	return &userClient{
		RestyClient: restyClient,
	}
}

func (restyClient *userClient) GetUser(ctx context.Context, body UserBody) (*io.ReadCloser, error) {
	log.Println("getting User!")
	restyClient.RestyClient.SetQueryParams(map[string]string{
		"results": "1",
		"gender":  body.Gender,
	})
	client := restyClient.RestyClient.GetClient()

	//resp, err := client.Get("https://randomuser.me/api/")
	resp, err := client.Get("http://localhost:4002")
	if err != nil {
		return nil, err
	}
	return &resp.Body, nil
}

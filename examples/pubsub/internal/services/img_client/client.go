package img_client

import (
	"context"
	"github.com/go-resty/resty/v2"
	"io"
	"log"
	"net/http"
)

type imageClient struct {
	RestyClient *resty.Client
}

type ImageClient interface {
	GetImage(ctx context.Context) (*io.ReadCloser, error)
}

func NewImageClient(client *http.Client) ImageClient {
	restyClient := resty.NewWithClient(client)
	return &imageClient{
		RestyClient: restyClient}
}

func (restyClient *imageClient) GetImage(ctx context.Context) (*io.ReadCloser, error) {
	log.Println("getting image!")
	client := restyClient.RestyClient.GetClient()
	//resp, err := client.Get("https://api.catboys.com/img")
	resp, err := client.Get("http://localhost:4003")

	if err != nil {
		return nil, err
	}
	return &resp.Body, nil
}

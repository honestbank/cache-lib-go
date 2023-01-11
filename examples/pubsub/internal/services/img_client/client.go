package img_client

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/go-resty/resty/v2"
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
	resp, err := client.Get("https://api.catboys.com/img")
	if err != nil {
		return nil, err
	}
	return &resp.Body, nil
}

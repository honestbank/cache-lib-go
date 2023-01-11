package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	os "os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/utils"
	imgClient "github.com/pubsub_example/internal/services/img_client"
	usrClient "github.com/pubsub_example/internal/services/user_client"

	cache_lib "github.com/honestbank/cache-lib-go"
)

type IMGResponse struct {
	Url       string `json:"url"`
	Artist    string `json:"artist"`
	ArtistUrl string `json:"artist_url"`
	SourceUrl string `json:"source_url"`
	Error     string `json:"error"`
}

type Response struct {
	Results []struct {
		Gender string `json:"gender"`
		Name   struct {
			Title string `json:"title"`
			First string `json:"first"`
			Last  string `json:"last"`
		} `json:"name"`
		Location struct {
			Street struct {
				Number int    `json:"number"`
				Name   string `json:"name"`
			} `json:"street"`
			City        string `json:"city"`
			State       string `json:"state"`
			Country     string `json:"country"`
			Coordinates struct {
				Latitude  string `json:"latitude"`
				Longitude string `json:"longitude"`
			} `json:"coordinates"`
			Timezone struct {
				Offset      string `json:"offset"`
				Description string `json:"description"`
			} `json:"timezone"`
		} `json:"location"`
		Email string `json:"email"`
		Login struct {
			Uuid     string `json:"uuid"`
			Username string `json:"username"`
			Password string `json:"password"`
			Salt     string `json:"salt"`
			Md5      string `json:"md5"`
			Sha1     string `json:"sha1"`
			Sha256   string `json:"sha256"`
		} `json:"login"`
		Dob struct {
			Date time.Time `json:"date"`
			Age  int       `json:"age"`
		} `json:"dob"`
		Registered struct {
			Date time.Time `json:"date"`
			Age  int       `json:"age"`
		} `json:"registered"`
		Phone string `json:"phone"`
		Cell  string `json:"cell"`
		Id    struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"id"`
		Picture struct {
			Large     string `json:"large"`
			Medium    string `json:"medium"`
			Thumbnail string `json:"thumbnail"`
		} `json:"picture"`
		Nat string `json:"nat"`
	} `json:"results"`
	Info struct {
		Seed    string `json:"seed"`
		Results int    `json:"results"`
		Page    int    `json:"page"`
		Version string `json:"version"`
	} `json:"info"`
}

type IMGBody struct {
}

func main() {
	imageClient := imgClient.NewImageClient(&http.Client{})
	userClient := usrClient.NewUserClient(&http.Client{})

	redisClient := redis.NewClient(&redis.Options{
		Addr: ":6379", // We connect to host redis, thats what the hostname of the redis service is set to in the docker-compose
		DB:   0,
	})

	app := fiber.New()

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Hello, World!")
	})

	app.Get("/img", func(c *fiber.Ctx) error {

		thecache := cache_lib.NewCache[IMGResponse](redisClient)
		data, err := thecache.RememberBlocking(c.Context(), func(ctx context.Context) (*IMGResponse, error) {
			data, err := imageClient.GetImage(c.Context())
			var response IMGResponse
			err = json.NewDecoder(*data).Decode(&response)
			if err != nil {
				fmt.Println(err)
				return nil, err
			}
			return &response, nil

		}, "img", 10*time.Second)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(data)

		return c.Send(body)

		//// Create a new RequestHerdCache using the provided NewHerdCache function
		//key := utils.CopyString(c.Query("key"))
		//log.Println("KEY", key)
		//fullKey := imgRequest.GetKey() + key
		//log.Println("FULLKEY", fullKey)
		//hasher := md5.New()
		//hasher.Write([]byte(fullKey))
		//
		//hashedKey := hex.EncodeToString(hasher.Sum(nil))
		//herdCache := herd.NewHerdCache[IMGBody, IMGResponse](imgRequest)
		//
		//// Create a new context with a timeout
		//ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		//defer cancel()
		//
		//// Use the HerdRequest method to send the request and acquire a lock
		//data, _ := herdCache.HerdRequest(ctx, hashedKey, nil)
		//stringbyte, _ := json.Marshal(data)
		//return c.SendString(string(stringbyte))
	})

	app.Get("/user", func(c *fiber.Ctx) error {

		gender := utils.CopyString(c.Query("gender"))
		body := usrClient.UserBody{Gender: gender}
		jsonbody, _ := json.Marshal(body)

		fullKey := "usr" + string(jsonbody)

		hasher := md5.New()
		hasher.Write([]byte(fullKey))

		hashedKey := hex.EncodeToString(hasher.Sum(nil))

		thecache := cache_lib.NewCache[Response](redisClient)
		data, err := thecache.RememberBlocking(c.Context(), func(ctx context.Context) (*Response, error) {
			data, err := userClient.GetUser(c.Context(), body)
			var response Response
			err = json.NewDecoder(*data).Decode(&response)
			if err != nil {
				fmt.Println(err)
				return nil, err
			}
			return &response, nil

		}, hashedKey, 10*time.Second)
		if err != nil {
			return err
		}
		respBody, _ := json.Marshal(data)

		return c.Send(respBody)

		//// Create a new RequestHerdCache using the provided NewHerdCache function
		//gender := utils.CopyString(c.Query("gender"))
		//body := usrClient.UserBody{Gender: gender}
		//jsonbody, _ := json.Marshal(body)
		//log.Println("KEY", jsonbody)
		//fullKey := userRequest.GetKey() + string(jsonbody)
		//log.Println("FULLKEY", fullKey)
		//hasher := md5.New()
		//hasher.Write([]byte(fullKey))
		//
		//hashedKey := hex.EncodeToString(hasher.Sum(nil))
		//herdCache := herd.NewHerdCache[usrClient.UserBody, Response](userRequest)
		//
		//// Create a new context with a timeout
		//ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		//defer cancel()
		//
		//// Use the HerdRequest method to send the request and acquire a lock
		//data, _ := herdCache.HerdRequest(ctx, hashedKey, &body)
		//stringbyte, _ := json.Marshal(data)
		//return c.SendString(string(stringbyte))
	})

	app.Listen(":" + os.Getenv("PORT"))
	//app.Listen(":3000")
}

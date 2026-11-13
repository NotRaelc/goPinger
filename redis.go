package main

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisClient *redis.Client

func initRedis(ctx context.Context, url string) error {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return err
	}
	redisClient = redis.NewClient(opts)
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return redisClient.Ping(c).Err()
}
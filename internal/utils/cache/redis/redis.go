package redis

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/langgenius/dify-plugin-daemon/internal/utils/cache"
	"github.com/langgenius/dify-plugin-daemon/internal/utils/log"
	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

type Context struct {
	redis.Pipeliner
}

func (c *Context) Get() cache.Client {
	return &Client{c.Pipeliner}
}

type Client struct {
	redis.Cmdable
}

func getRedisOptions(addr, username, password string, useSsl bool, db int) *redis.Options {
	opts := &redis.Options{
		Addr:     addr,
		Username: username,
		Password: password,
		DB:       db,
	}
	if useSsl {
		opts.TLSConfig = &tls.Config{}
	}
	return opts
}

func InitRedisClient(addr, username, password string, useSsl bool, db int) error {
	opts := getRedisOptions(addr, username, password, useSsl, db)
	client := redis.NewClient(opts)

	if _, err := client.Ping(ctx).Result(); err != nil {
		return err
	}

	cache.SetClient(&Client{client})
	return nil
}

func (c *Client) Close() error {
	client := c.Cmdable.(*redis.Client)
	return client.Close()
}

func (c *Client) Set(key string, value any, time time.Duration) error {
	return c.Cmdable.Set(ctx, key, value, time).Err()
}

func (c *Client) GetBytes(key string) ([]byte, error) {
	val, err := c.Cmdable.Get(ctx, key).Bytes()
	if err != nil && err == redis.Nil {
		return nil, cache.ErrNotFound
	}
	return val, err
}

func (c *Client) GetString(key string) (string, error) {
	val, err := c.Cmdable.Get(ctx, key).Result()
	if err != nil && err == redis.Nil {
		return "", cache.ErrNotFound
	}
	return val, err
}

func (c *Client) Delete(key string) (int64, error) {
	return c.Cmdable.Del(ctx, key).Result()
}

func (c *Client) Count(key ...string) (int64, error) {
	return c.Cmdable.Exists(ctx, key...).Result()
}

func (c *Client) SetMapField(key string, field string, value string) error {
	return c.Cmdable.HSet(ctx, key, field, value).Err()
}

func (c *Client) GetMapField(key string, field string) (string, error) {
	val, err := c.Cmdable.HGet(ctx, key, field).Result()
	if err != nil && err == redis.Nil {
		return "", cache.ErrNotFound
	}
	return val, err
}

func (c *Client) DeleteMapField(key string, field string) error {
	return c.Cmdable.HDel(ctx, key, field).Err()
}

func (c *Client) GetMap(key string) (map[string]string, error) {
	val, err := c.Cmdable.HGetAll(ctx, key).Result()
	if err != nil && err == redis.Nil {
		return nil, cache.ErrNotFound
	}
	return val, err
}

func (c *Client) ScanMapStream(key string, cursor uint64, match string, count int64) ([]string, uint64, error) {
	return c.Cmdable.HScan(ctx, key, cursor, match, count).Result()
}

func (c *Client) SetNX(key string, value any, time time.Duration) (bool, error) {
	return c.Cmdable.SetNX(ctx, key, value, time).Result()
}

func (c *Client) Expire(key string, time time.Duration) (bool, error) {
	return c.Cmdable.Expire(ctx, key, time).Result()
}

func (c *Client) Transaction(fn func(context cache.Context) error) error {
	client := c.Cmdable.(*redis.Client)
	return client.Watch(ctx, func(tx *redis.Tx) error {
		_, err := tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			return fn(&Context{p})
		})
		if err == redis.Nil {
			return nil
		}
		return err
	})
}

func (c *Client) Publish(channel string, message string) error {
	return c.Cmdable.Publish(ctx, channel, message).Err()
}

func (c *Client) Subscribe(channel string) (<-chan string, func()) {
	client := c.Cmdable.(*redis.Client)
	pubsub := client.Subscribe(ctx, channel)
	ch := make(chan string)
	connectionEstablished := make(chan bool)

	go func() {
		defer close(ch)
		defer close(connectionEstablished)

		alive := true
		for alive {
			iface, err := pubsub.Receive(context.Background())
			if err != nil {
				log.Error("failed to receive message from redis: %s, will retry in 1 second", err.Error())
				time.Sleep(1 * time.Second)
				continue
			}
			switch data := iface.(type) {
			case *redis.Subscription:
				connectionEstablished <- true
			case *redis.Message:
				ch <- data.Payload
			case *redis.Pong:
			default:
				alive = false
			}
		}
	}()

	// wait for the connection to be established
	<-connectionEstablished

	return ch, func() {
		_ = pubsub.Close()
	}
}

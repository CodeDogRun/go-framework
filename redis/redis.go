package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CodeDogRun/go-framework/config"
	goRedis "github.com/redis/go-redis/v9"
)

var (
	ctx      = context.Background()
	keys     []string
	wrappers = make(map[string]*goRedis.Client)
	chains   sync.Map
)

type Chain struct {
	client *goRedis.Client
}

func Init() {
	for key := range config.GetStringMap("redis") {
		rdb := goRedis.NewClient(&goRedis.Options{
			Addr:     config.GetString("redis." + key + ".addr"),
			Username: config.GetString("redis." + key + ".username"),
			Password: config.GetString("redis." + key + ".password"),
			DB:       config.GetInt("redis." + key + ".db"),
		})

		_, err := rdb.Ping(ctx).Result()
		if err != nil {
			panic(fmt.Sprintf("Redis连接失败: %v", err))
		}
		keys = append(keys, key)
		wrappers[key] = rdb
	}
}

func Connect(node ...string) *Chain {
	if len(keys) == 0 {
		panic("尚未创建Redis连接, 请检查配置文件！")
	}

	key := "master"
	if len(node) > 0 {
		key = node[0]
	}

	if val, ok := chains.Load(key); ok {
		return val.(*Chain)
	}

	client, ok := wrappers[key]
	if !ok {
		panic(fmt.Sprintf("不存在的Redis连接: %s", key))
	}

	chain := &Chain{client: client}
	chains.Store(key, chain)
	return chain
}

func (c *Chain) GetRDB() *goRedis.Client {
	return c.client
}

func (c *Chain) Keys(pattern string) ([]string, error) {
	return c.client.Keys(ctx, pattern).Result()
}

func (c *Chain) Scan(cursor uint64, match string, count int64) *goRedis.ScanCmd {
	return c.client.Scan(ctx, cursor, match, count)
}

func (c *Chain) Set(key string, value any, expiration time.Duration) error {
	return c.client.Set(ctx, key, value, expiration).Err()
}

func (c *Chain) GetString(key string) string {
	return c.client.Get(ctx, key).Val()
}

func (c *Chain) GetInt64(key string) int64 {
	i, err := c.client.Get(ctx, key).Int64()
	if err != nil {
		return 0
	}
	return i
}

func (c *Chain) GetInt(key string) int {
	i, err := c.client.Get(ctx, key).Int()
	if err != nil {
		return 0
	}
	return i
}

func (c *Chain) GetTTL(key string) time.Duration {
	return c.client.TTL(ctx, key).Val()
}

func (c *Chain) GetPTTL(key string) time.Duration {
	return c.client.PTTL(ctx, key).Val()
}

func (c *Chain) GetBool(key string) bool {
	i, err := c.client.Get(ctx, key).Bool()
	if err != nil {
		return false
	}
	return i
}

func (c *Chain) GetFloat32(key string) float32 {
	i, err := c.client.Get(ctx, key).Float32()
	if err != nil {
		return 0
	}
	return i
}

func (c *Chain) GetFloat64(key string) float64 {
	i, err := c.client.Get(ctx, key).Float64()
	if err != nil {
		return 0
	}
	return i
}

func (c *Chain) GetBytes(key string) []byte {
	i, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil
	}
	return i
}

func (c *Chain) GetUint64(key string) uint64 {
	i, err := c.client.Get(ctx, key).Uint64()
	if err != nil {
		return 0
	}
	return i
}

func (c *Chain) Del(key ...string) {
	c.client.Del(ctx, key...)
}

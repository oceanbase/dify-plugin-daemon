package mysql

import (
	"fmt"
	"strings"
	"time"

	"github.com/langgenius/dify-plugin-daemon/internal/db"
	"github.com/langgenius/dify-plugin-daemon/internal/utils/cache"
	"github.com/langgenius/dify-plugin-daemon/internal/utils/log"
	"gorm.io/gorm"
)

func cleanMessages() {
	time.Sleep(time.Minute * 5)

	for {
		log.Info("cleaning outdated cache and messages")
		now := time.Now()

		result := db.DifyPluginDB.Where("expire_time <= ?", now).Delete(&CacheKV{})
		if result.Error != nil {
			log.Error("failed to clean expired kv cache: %v", result.Error)
		} else {
			log.Info("cleaned %d expired kv cache", result.RowsAffected)
		}
		time.Sleep(time.Minute * 1)
	}
}

type Context struct {
	*gorm.DB
}

type Client struct {
	*gorm.DB
}

func (c *Context) Get() cache.Client {
	return &Client{c.DB}
}

func InitMysqlClient() {
	cache.SetClient(&Client{db.DifyPluginDB})
	go cleanMessages()
}

func toBytes(data any) []byte {
	if bytes, ok := data.([]byte); ok {
		return bytes
	} else if str, ok := data.(string); ok {
		return []byte(str)
	} else {
		return nil
	}
}

func convertRegexToSQL(pattern string) string {
	return strings.ReplaceAll(pattern, "*", "%")
}

func (c Client) Close() error {
	return nil
}

func (c Client) Set(key string, value any, expire time.Duration) error {
	val := toBytes(value)
	expireTime := time.Now().Add(expire)

	// 使用 INSERT ... ON DUPLICATE KEY UPDATE 来避免并发写入问题
	sql := `INSERT INTO cache_kvs (cache_key, cache_value, expire_time, created_at, updated_at) 
			VALUES (?, ?, ?, NOW(), NOW()) 
			ON DUPLICATE KEY UPDATE 
			cache_value = VALUES(cache_value), 
			expire_time = VALUES(expire_time), 
			updated_at = NOW()`

	return c.DB.Exec(sql, key, val, expireTime).Error
}

func (c Client) GetBytes(key string) ([]byte, error) {
	var cacheKV CacheKV
	result := c.DB.Where("cache_key = ? AND expire_time > ?", key, time.Now()).First(&cacheKV)
	if result.Error != nil {
		if result.Error.Error() == "record not found" {
			return nil, cache.ErrNotFound
		}
		return nil, result.Error
	}

	return cacheKV.CacheValue, nil
}

func (c Client) GetString(key string) (string, error) {
	bytes, err := c.GetBytes(key)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (c Client) Delete(key string) (int64, error) {
	result := c.DB.Where("cache_key = ?", key).Delete(&CacheKV{})
	return result.RowsAffected, result.Error
}

func (c Client) Count(key ...string) (int64, error) {
	var count int64
	query := c.DB.Model(&CacheKV{}).Where("expire_time > ?", time.Now())

	if len(key) > 0 {
		query = query.Where("cache_key IN ?", key)
	}

	result := query.Count(&count)
	return count, result.Error
}

func (c Client) SetMapField(key string, field string, value string) error {
	// 使用 INSERT ... ON DUPLICATE KEY UPDATE 来避免并发写入问题
	sql := `INSERT INTO cache_maps (cache_key, cache_field, cache_value, created_at, updated_at) 
			VALUES (?, ?, ?, NOW(), NOW()) 
			ON DUPLICATE KEY UPDATE 
			cache_value = VALUES(cache_value), 
			updated_at = NOW()`

	return c.DB.Exec(sql, key, field, value).Error
}

func (c Client) GetMapField(key string, field string) (string, error) {
	var cacheMap CacheMap
	result := c.DB.Where("cache_key = ? AND cache_field = ?", key, field).First(&cacheMap)
	if result.Error != nil {
		if result.Error.Error() == "record not found" {
			return "", cache.ErrNotFound
		}
		return "", result.Error
	}

	return cacheMap.CacheValue, nil
}

func (c Client) DeleteMapField(key string, field string) error {
	result := c.DB.Where("cache_key = ? AND cache_field = ?", key, field).Delete(&CacheMap{})
	return result.Error
}

func (c Client) GetMap(key string) (map[string]string, error) {
	var cacheMaps []CacheMap
	result := c.DB.Where("cache_key = ?", key).Find(&cacheMaps)
	if result.Error != nil {
		return nil, result.Error
	}

	resultMap := make(map[string]string)
	for _, cacheMap := range cacheMaps {
		resultMap[cacheMap.CacheField] = cacheMap.CacheValue
	}

	if len(resultMap) == 0 {
		return nil, cache.ErrNotFound
	}

	return resultMap, nil
}

func (c Client) ScanMapStream(key string, cursor uint64, match string, count int64) ([]string, uint64, error) {
	var cacheMaps []CacheMap
	query := c.DB.Where("cache_key = ?", key)

	if match != "" {
		sqlPattern := convertRegexToSQL(match)
		query = query.Where("cache_field LIKE ?", sqlPattern)
	}

	query = query.Offset(int(cursor)).Limit(int(count))

	result := query.Find(&cacheMaps)
	if result.Error != nil {
		return nil, 0, result.Error
	}

	var keys []string
	for _, cacheMap := range cacheMaps {
		keys = append(keys, cacheMap.CacheField, cacheMap.CacheValue)
	}

	nextCursor := cursor + uint64(len(cacheMaps))
	if len(cacheMaps) < int(count) {
		nextCursor = 0
	}

	return keys, nextCursor, nil
}

func (c Client) SetNX(key string, value any, expire time.Duration) (bool, error) {
	val := toBytes(value)
	expireTime := time.Now().Add(expire)

	// 使用 INSERT IGNORE 来实现 SetNX，避免并发写入问题
	sql := `INSERT IGNORE INTO cache_kvs (cache_key, cache_value, expire_time, created_at, updated_at) 
			VALUES (?, ?, ?, NOW(), NOW())`

	result := c.DB.Exec(sql, key, val, expireTime)
	if result.Error != nil {
		return false, result.Error
	}

	// 如果影响的行数为1，说明插入成功；如果为0，说明记录已存在
	return result.RowsAffected == 1, nil
}

func (c Client) Expire(key string, expire time.Duration) (bool, error) {
	expireTime := time.Now().Add(expire)

	result := c.DB.Model(&CacheKV{}).
		Where("cache_key = ?", key).
		Update("expire_time", expireTime)

	return result.RowsAffected > 0, result.Error
}

func (c Client) Transaction(fn func(context cache.Context) error) error {
	return c.DB.Transaction(func(tx *gorm.DB) error {
		context := &Context{tx}
		return fn(context)
	})
}

func (c Client) Publish(channel string, message string) error {
	msg := Message{
		Channel: channel,
		Message: message,
	}

	result := c.DB.Create(&msg)
	if result.Error != nil {
		return result.Error
	}

	return nil
}

func (c Client) Subscribe(channel string) (<-chan string, func()) {
	ch := make(chan string, 100)
	stop := make(chan bool)

	subscriber := fmt.Sprintf("sub_%d", time.Now().UnixNano())
	var subscription MessageSubscribe
	c.DB.Model(&MessageSubscribe{}).
		Where("channel = ? AND subscriber = ?", channel, subscriber).
		Assign(MessageSubscribe{
			Channel:       channel,
			Subscriber:    subscriber,
			LastMessageId: -1,
		}).
		FirstOrCreate(&subscription)

	go func() {
		defer close(ch)
		defer func() {
			c.DB.Where("channel = ? AND subscriber = ?", channel, subscriber).Delete(&MessageSubscribe{})
		}()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var messages []Message
				result := c.DB.Where("channel = ? AND id > ?", channel, subscription.LastMessageId).
					Order("id ASC").
					Limit(10).
					Find(&messages)

				if result.Error != nil {
					continue
				}

				for _, msg := range messages {
					select {
					case ch <- msg.Message:
						subscription.LastMessageId = msg.ID
						c.DB.Model(&MessageSubscribe{}).
							Where("channel = ? AND subscriber = ?", channel, subscriber).
							Update("last_message_id", msg.ID)
					case <-stop:
						return
					}
				}
			}
		}
	}()

	return ch, func() {
		close(stop)
	}
}

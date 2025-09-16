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

	// Use INSERT ... ON DUPLICATE KEY UPDATE to avoid concurrent write issues
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
	// Use INSERT ... ON DUPLICATE KEY UPDATE to avoid concurrent write issues
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

	// Use INSERT IGNORE to implement SetNX, avoiding concurrent write issues
	sql := `INSERT IGNORE INTO cache_kvs (cache_key, cache_value, expire_time, created_at, updated_at) 
			VALUES (?, ?, ?, NOW(), NOW())`

	result := c.DB.Exec(sql, key, val, expireTime)
	if result.Error != nil {
		return false, result.Error
	}

	// If affected rows is 1, insertion succeeded; if 0, record already exists
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

// Increase increases the key value by 1
func (c Client) Increase(key string) (int64, error) {
	// MySQL implementation: first try to get current value, then increment
	var cacheKV CacheKV
	result := c.DB.Where("cache_key = ? AND expire_time > ?", key, time.Now()).First(&cacheKV)
	if result.Error != nil {
		if result.Error.Error() == "record not found" {
			// If not exists, create new record with value 1
			expireTime := time.Now().Add(time.Hour * 24) // Default 24 hours expiration
			sql := `INSERT INTO cache_kvs (cache_key, cache_value, expire_time, created_at, updated_at) 
					VALUES (?, ?, ?, NOW(), NOW()) 
					ON DUPLICATE KEY UPDATE 
					cache_value = CAST(cache_value AS UNSIGNED) + 1, 
					updated_at = NOW()`
			err := c.DB.Exec(sql, key, []byte("1"), expireTime).Error
			if err != nil {
				return 0, err
			}
			return 1, nil
		}
		return 0, result.Error
	}

	// Increment existing value
	sql := `UPDATE cache_kvs 
			SET cache_value = CAST(cache_value AS UNSIGNED) + 1, 
				updated_at = NOW() 
			WHERE cache_key = ? AND expire_time > ?`
	result = c.DB.Exec(sql, key, time.Now())
	if result.Error != nil {
		return 0, result.Error
	}

	// Get new value
	var newValue int64
	err := c.DB.Model(&CacheKV{}).
		Select("CAST(cache_value AS UNSIGNED)").
		Where("cache_key = ? AND expire_time > ?", key, time.Now()).
		Scan(&newValue).Error
	if err != nil {
		return 0, err
	}

	return newValue, nil
}

// Decrease decreases the key value by 1
func (c Client) Decrease(key string) (int64, error) {
	// MySQL implementation: first try to get current value, then decrement
	var cacheKV CacheKV
	result := c.DB.Where("cache_key = ? AND expire_time > ?", key, time.Now()).First(&cacheKV)
	if result.Error != nil {
		if result.Error.Error() == "record not found" {
			return 0, cache.ErrNotFound
		}
		return 0, result.Error
	}

	// Decrement existing value
	sql := `UPDATE cache_kvs 
			SET cache_value = CAST(cache_value AS UNSIGNED) - 1, 
				updated_at = NOW() 
			WHERE cache_key = ? AND expire_time > ?`
	result = c.DB.Exec(sql, key, time.Now())
	if result.Error != nil {
		return 0, result.Error
	}

	// Get new value
	var newValue int64
	err := c.DB.Model(&CacheKV{}).
		Select("CAST(cache_value AS UNSIGNED)").
		Where("cache_key = ? AND expire_time > ?", key, time.Now()).
		Scan(&newValue).Error
	if err != nil {
		return 0, err
	}

	return newValue, nil
}

// SetExpire sets the expire time for the key
func (c Client) SetExpire(key string, expire time.Duration) error {
	expireTime := time.Now().Add(expire)
	result := c.DB.Model(&CacheKV{}).
		Where("cache_key = ?", key).
		Update("expire_time", expireTime)
	return result.Error
}

// ScanKeys scans keys with match pattern
func (c Client) ScanKeys(match string) ([]string, error) {
	var cacheKVs []CacheKV
	query := c.DB.Model(&CacheKV{}).Where("expire_time > ?", time.Now())

	if match != "" {
		sqlPattern := convertRegexToSQL(match)
		query = query.Where("cache_key LIKE ?", sqlPattern)
	}

	result := query.Find(&cacheKVs)
	if result.Error != nil {
		return nil, result.Error
	}

	var keys []string
	for _, cacheKV := range cacheKVs {
		keys = append(keys, cacheKV.CacheKey)
	}

	return keys, nil
}

// ScanKeysAsync scans keys with match pattern asynchronously
func (c Client) ScanKeysAsync(match string, fn func([]string) error) error {
	keys, err := c.ScanKeys(match)
	if err != nil {
		return err
	}
	return fn(keys)
}

// SetMapFields sets multiple map fields at once
func (c Client) SetMapFields(key string, v map[string]any) error {
	// MySQL implementation: batch insert or update
	for field, value := range v {
		valueStr := fmt.Sprintf("%v", value)
		err := c.SetMapField(key, field, valueStr)
		if err != nil {
			return err
		}
	}
	return nil
}

// Lock implements distributed locking
func (c Client) Lock(key string, expire time.Duration, tryLockTimeout time.Duration) error {
	lockKey := fmt.Sprintf("lock:%s", key)
	
	// Try to acquire lock
	success, err := c.SetNX(lockKey, "1", expire)
	if err != nil {
		return err
	}
	
	if success {
		return nil
	}
	
	// If acquisition fails, wait and retry
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	
	for range ticker.C {
		success, err := c.SetNX(lockKey, "1", expire)
		if err != nil {
			return err
		}
		if success {
			return nil
		}
		
		tryLockTimeout -= 20 * time.Millisecond
		if tryLockTimeout <= 0 {
			return cache.ErrNotFound // Use existing error type
		}
	}
	
	return nil
}

// Unlock releases the distributed lock
func (c Client) Unlock(key string) error {
	lockKey := fmt.Sprintf("lock:%s", key)
	_, err := c.Delete(lockKey)
	return err
}

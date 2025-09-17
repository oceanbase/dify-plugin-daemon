package cache

import (
	"errors"
	"strings"
	"time"

	"github.com/langgenius/dify-plugin-daemon/internal/utils/parser"
)

type Context interface {
	Get() Client
}

type Client interface {
	Close() error
	Set(key string, value any, time time.Duration) error
	GetBytes(key string) ([]byte, error)
	GetString(key string) (string, error)
	Delete(key string) (int64, error)
	Count(key ...string) (int64, error)
	SetMapField(key string, field string, value string) error
	GetMapField(key string, field string) (string, error)
	DeleteMapField(key string, field string) error
	GetMap(key string) (map[string]string, error)
	ScanMapStream(key string, cursor uint64, match string, count int64) ([]string, uint64, error)
	SetNX(key string, value any, time time.Duration) (bool, error)
	Expire(key string, time time.Duration) (bool, error)
	Transaction(fn func(context Context) error) error
	Publish(channel string, message string) error
	Subscribe(channel string) (<-chan string, func())
	// Additional methods from the original redis.go
	Increase(key string) (int64, error)
	Decrease(key string) (int64, error)
	SetExpire(key string, time time.Duration) error
	ScanKeys(match string) ([]string, error)
	ScanKeysAsync(match string, fn func([]string) error) error
	SetMapFields(key string, v map[string]any) error
	Lock(key string, expire time.Duration, tryLockTimeout time.Duration) error
	Unlock(key string) error
}

var (
	client Client

	ErrNotInit  = errors.New("cache not init")
	ErrNotFound = errors.New("cache not found")
)

func SetClient(c Client) {
	client = c
}

// Close closes the cache client
func Close() error {
	if client == nil {
		return ErrNotInit
	}

	return client.Close()
}

func getCmdable(context ...Context) Client {
	if len(context) > 0 {
		return context[0].Get()
	}

	return client
}

func serialKey(keys ...string) string {
	return strings.Join(append(
		[]string{"plugin_daemon"},
		keys...,
	), ":")
}

// Store stores the key-value pair
func Store(key string, value any, time time.Duration, context ...Context) error {
	return store(serialKey(key), value, time, context...)
}

// store stores the key-value pair without serialKey
func store(key string, value any, time time.Duration, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}
	if _, ok := value.(string); !ok {
		var err error
		value, err = parser.MarshalCBOR(value)
		if err != nil {
			return err
		}
	}

	return getCmdable(context...).Set(key, value, time)
}

// Get gets the value
func Get[T any](key string, context ...Context) (*T, error) {
	return get[T](serialKey(key), context...)
}

// Get gets the value without serialKey
func get[T any](key string, context ...Context) (*T, error) {
	if client == nil {
		return nil, ErrNotInit
	}

	val, err := getCmdable(context...).GetBytes(key)
	if err != nil {
		return nil, err
	}

	if len(val) == 0 {
		return nil, ErrNotFound
	}

	result, err := parser.UnmarshalCBOR[T](val)
	return &result, err
}

// GetString gets the string value
func GetString(key string, context ...Context) (string, error) {
	if client == nil {
		return "", ErrNotInit
	}

	return getCmdable(context...).GetString(serialKey(key))
}

// Del deletes the key
func Del(key string, context ...Context) (int64, error) {
	return del(serialKey(key), context...)
}

// del deletes the key without serialKey
func del(key string, context ...Context) (int64, error) {
	if client == nil {
		return 0, ErrNotInit
	}
	return getCmdable(context...).Delete(key)
}

// Exist checks the key exist or not
func Exist(key string, context ...Context) (int64, error) {
	if client == nil {
		return 0, ErrNotInit
	}

	return getCmdable(context...).Count(serialKey(key))
}

// SetMapOneField set the map field with key
func SetMapOneField(key string, field string, value any, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	str, ok := value.(string)
	if !ok {
		str = parser.MarshalJson(value)
	}
	return getCmdable(context...).SetMapField(serialKey(key), field, str)
}

// GetMapField get the map field with key
func GetMapField[T any](key string, field string, context ...Context) (*T, error) {
	if client == nil {
		return nil, ErrNotInit
	}

	val, err := getCmdable(context...).GetMapField(serialKey(key), field)
	if err != nil {
		return nil, err
	}

	result, err := parser.UnmarshalJson[T](val)
	return &result, err
}

// DelMapField delete the map field with key
func DelMapField(key string, field string, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	return getCmdable(context...).DeleteMapField(serialKey(key), field)
}

// GetMap get the map with key
func GetMap[V any](key string, context ...Context) (map[string]V, error) {
	if client == nil {
		return nil, ErrNotInit
	}

	val, err := getCmdable(context...).GetMap(serialKey(key))
	if err != nil {
		return nil, err
	}

	result := make(map[string]V)
	for k, v := range val {
		value, err := parser.UnmarshalJson[V](v)
		if err != nil {
			continue
		}

		result[k] = value
	}

	return result, nil
}

// ScanMap scan the map with match pattern, format like "key*"
func ScanMap[V any](key string, match string, context ...Context) (map[string]V, error) {
	if client == nil {
		return nil, ErrNotInit
	}

	result := make(map[string]V)

	err := ScanMapAsync[V](key, match, func(m map[string]V) error {
		for k, v := range m {
			result[k] = v
		}

		return nil
	})

	return result, err
}

// ScanMapAsync scan the map with match pattern, format like "key*"
func ScanMapAsync[V any](key string, match string, fn func(map[string]V) error, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	cursor := uint64(0)

	for {
		kvs, newCursor, err := getCmdable(context...).
			ScanMapStream(serialKey(key), cursor, match, 32)
		if err != nil {
			return err
		}

		result := make(map[string]V)
		for i := 0; i < len(kvs); i += 2 {
			value, err := parser.UnmarshalJson[V](kvs[i+1])
			if err != nil {
				continue
			}

			result[kvs[i]] = value
		}

		if err := fn(result); err != nil {
			return err
		}

		if newCursor == 0 {
			break
		}

		cursor = newCursor
	}

	return nil
}

// SetNX set the key-value pair with expire time
func SetNX[T any](key string, value T, expire time.Duration, context ...Context) (bool, error) {
	if client == nil {
		return false, ErrNotInit
	}

	// marshal the value
	bytes, err := parser.MarshalCBOR(value)
	if err != nil {
		return false, err
	}

	return getCmdable(context...).SetNX(serialKey(key), bytes, expire)
}


func Expire(key string, time time.Duration, context ...Context) (bool, error) {
	if client == nil {
		return false, ErrNotInit
	}

	return getCmdable(context...).Expire(serialKey(key), time)
}

func Transaction(fn func(ctx Context) error) error {
	if client == nil {
		return ErrNotInit
	}

	return client.Transaction(fn)
}

func Publish(channel string, message any) error {
	if client == nil {
		return ErrNotInit
	}

	str, ok := message.(string)
	if !ok {
		str = parser.MarshalJson(message)
	}

	return client.Publish(channel, str)
}

func Subscribe[T any](channel string) (<-chan T, func()) {
	strCh, fn := client.Subscribe(channel)
	ch := make(chan T)
	go func() {
		defer close(ch)
		for s := range strCh {
			v, err := parser.UnmarshalJson[T](s)
			if err != nil {
				continue
			}
			ch <- v
		}
	}()

	return ch, fn
}

// Increase increases the key value by 1
func Increase(key string, context ...Context) (int64, error) {
	if client == nil {
		return 0, ErrNotInit
	}

	return getCmdable(context...).Increase(serialKey(key))
}

// Decrease decreases the key value by 1
func Decrease(key string, context ...Context) (int64, error) {
	if client == nil {
		return 0, ErrNotInit
	}

	return getCmdable(context...).Decrease(serialKey(key))
}

// SetExpire sets the expire time for the key
func SetExpire(key string, time time.Duration, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	return getCmdable(context...).SetExpire(serialKey(key), time)
}

// ScanKeys scans keys with match pattern
func ScanKeys(match string, context ...Context) ([]string, error) {
	if client == nil {
		return nil, ErrNotInit
	}

	result := make([]string, 0)

	if err := ScanKeysAsync(match, func(keys []string) error {
		result = append(result, keys...)
		return nil
	}, context...); err != nil {
		return nil, err
	}

	return result, nil
}

// ScanKeysAsync scans keys with match pattern asynchronously
func ScanKeysAsync(match string, fn func([]string) error, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	return getCmdable(context...).ScanKeysAsync(serialKey(match), fn)
}

// SetMapFields sets multiple map fields at once
func SetMapFields(key string, v map[string]any, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	return getCmdable(context...).SetMapFields(serialKey(key), v)
}

var (
	ErrLockTimeout = errors.New("lock timeout")
)

// Lock implements distributed locking
func Lock(key string, expire time.Duration, tryLockTimeout time.Duration, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	return getCmdable(context...).Lock(serialKey(key), expire, tryLockTimeout)
}

// Unlock releases the distributed lock
func Unlock(key string, context ...Context) error {
	if client == nil {
		return ErrNotInit
	}

	return getCmdable(context...).Unlock(serialKey(key))
}

package redis

import (
	"errors"
	"testing"

	"github.com/langgenius/dify-plugin-daemon/internal/utils/cache"
)

type TestAutoTypeStruct struct {
	ID string `json:"id"`
}

func TestAutoType(t *testing.T) {
	if err := InitRedisClient("127.0.0.1:6379", "", "difyai123456", false, 0); err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	err := cache.AutoSet("test", TestAutoTypeStruct{ID: "123"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := cache.AutoGet[TestAutoTypeStruct]("test")
	if err != nil {
		t.Fatal(err)
	}

	if result.ID != "123" {
		t.Fatal("result not correct")
	}

	if _, err := cache.AutoDelete[TestAutoTypeStruct]("test"); err != nil {
		t.Fatal(err)
	}
}

func TestAutoTypeWithGetter(t *testing.T) {
	if err := InitRedisClient("127.0.0.1:6379", "", "difyai123456", false, 0); err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	result, err := cache.AutoGetWithGetter("test1", func() (*TestAutoTypeStruct, error) {
		return &TestAutoTypeStruct{
			ID: "123",
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err = cache.AutoGetWithGetter("test1", func() (*TestAutoTypeStruct, error) {
		return nil, errors.New("must hit cache")
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.AutoDelete[TestAutoTypeStruct]("test1"); err != nil {
		t.Fatal(err)
	}

	if result.ID != "123" {
		t.Fatal("result not correct")
	}
}

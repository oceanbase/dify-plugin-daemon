package mysql

import (
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/langgenius/dify-plugin-daemon/internal/db"
	"github.com/langgenius/dify-plugin-daemon/internal/db/mysql"
	"github.com/langgenius/dify-plugin-daemon/internal/types/app"
	"github.com/langgenius/dify-plugin-daemon/internal/types/models"
	"github.com/langgenius/dify-plugin-daemon/internal/utils/cache"
	"github.com/stretchr/testify/assert"
)

type TestModel struct {
	models.Model
}

func init() {
	config := &app.Config{
		DBType:     "mysql",
		DBUsername: "root",
		DBPassword: "difyai123456",
		DBHost:     "0.0.0.0",
		DBPort:     3306,
		DBDatabase: "testing",
		DBSslMode:  "disable",
	}
	var err error
	db.DifyPluginDB, err = mysql.InitPluginDB(&mysql.MySQLConfig{
		Host:               config.DBHost,
		Port:               int(config.DBPort),
		DBName:             config.DBDatabase,
		DefaultDBName:      config.DBDefaultDatabase,
		User:               config.DBUsername,
		Pass:               config.DBPassword,
		SSLMode:            config.DBSslMode,
		MaxIdleConns:       config.DBMaxIdleConns,
		MaxOpenConns:       config.DBMaxOpenConns,
		ConnMaxLifetime:    config.DBConnMaxLifetime,
	})
	if err != nil {
		log.Panicf("failed init plugin db: %v", err)
	}
	InitMysqlClient()
}

func TestMysqlCacheKV(t *testing.T) {
	if err := db.DifyPluginDB.AutoMigrate(
		CacheKV{},
	); err != nil {
		t.Errorf("failed to auto migrate cache tables: %v", err)
	}
	defer func() {
		db.DifyPluginDB.Unscoped().Where("1 = 1").Delete(&CacheKV{})
	}()

	// non existent key
	_, err := cache.GetString("non_existent_key")
	assert.Equal(t, cache.ErrNotFound, err)

	// string value
	strKey, strValue := "test_string_key", "test_string_value"
	err = cache.Store(strKey, strValue, time.Minute*5)
	assert.NoError(t, err)
	value, err := cache.GetString(strKey)
	assert.NoError(t, err)
	assert.Equal(t, strValue, value)

	// object value
	modelKey, modelValue := "test_model_key", TestModel{models.Model{
		ID:        "test",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}}
	err = cache.Store(modelKey, modelValue, time.Minute*5)
	assert.NoError(t, err)
	val, err := cache.Get[TestModel](modelKey)
	assert.NoError(t, err)
	assert.Equal(t, modelValue.ID, val.ID)

	// exist and delete
	num, err := cache.Exist(strKey)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), num)
	num, err = cache.Del(strKey)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), num)
	num, err = cache.Exist(strKey)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), num)
}

func TestMysqlCacheMap(t *testing.T) {
	if err := db.DifyPluginDB.AutoMigrate(
		CacheMap{},
	); err != nil {
		t.Errorf("failed to auto migrate cache tables: %v", err)
	}
	defer func() {
		db.DifyPluginDB.Unscoped().Where("1 = 1").Delete(&CacheMap{})
	}()

	mapKey := "map_key"
	err := cache.SetMapOneField(
		mapKey,
		"map_field_1",
		TestModel{models.Model{
			ID:        "test_id_1",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	)
	assert.NoError(t, err)
	val, err := cache.GetMapField[TestModel](mapKey, "map_field_1")
	assert.NoError(t, err)
	assert.Equal(t, "test_id_1", val.ID)

	err = cache.SetMapOneField(
		mapKey,
		"map_field_2",
		TestModel{models.Model{
			ID:        "test_id_2",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	)
	assert.NoError(t, err)

	m, err := cache.GetMap[TestModel](mapKey)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(m))

	m, err = cache.ScanMap[TestModel](mapKey, "map_field*")
	assert.NoError(t, err)
	assert.Equal(t, 2, len(m))

	err = cache.DelMapField(mapKey, "map_field_2")
	assert.NoError(t, err)
	m, err = cache.ScanMap[TestModel](mapKey, "map_field*")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(m))
}

func TestMysqlPubSub(t *testing.T) {
	if err := db.DifyPluginDB.AutoMigrate(
		Message{},
		MessageSubscribe{},
	); err != nil {
		t.Errorf("failed to auto migrate pubsub tables: %v", err)
	}
	defer func() {
		db.DifyPluginDB.Unscoped().Where("1 = 1").Delete(&Message{})
		db.DifyPluginDB.Unscoped().Where("1 = 1").Delete(&MessageSubscribe{})
	}()

	ch := "test-channel-p2a"

	wg := sync.WaitGroup{}
	wg.Add(3)

	swg := sync.WaitGroup{}
	swg.Add(3)

	for i := 0; i < 3; i++ {
		go func() {
			sub, cancel := cache.Subscribe[TestModel](ch)
			swg.Done()
			defer cancel()
			for j := 0; j < 5; j++ {
				<-sub
			}
			wg.Done()
		}()
	}

	swg.Wait()

	for i := 0; i < 5; i++ {
		err := cache.Publish(ch, TestModel{models.Model{
			ID: "test_id_" + fmt.Sprintf("%d", i),
		}})
		if err != nil {
			t.Errorf("publish failed: %v", err)
		}
	}

	wg.Wait()
}

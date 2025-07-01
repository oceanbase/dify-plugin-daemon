package mysql

import "time"

type CacheKV struct {
	ID         int64     `json:"id" gorm:"column:id;primaryKey;type:bigint(20) auto_increment"`
	CacheKey   string    `json:"cache_key" gorm:"column:cache_key;type:varchar(256);not null;unique"`
	CacheValue []byte    `json:"cache_value" gorm:"column:cache_value;type:blob;not null"`
	ExpireTime time.Time `json:"expire_time" gorm:"index"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CacheMap struct {
	ID         int64     `json:"id" gorm:"column:id;primaryKey;type:bigint(20) auto_increment"`
	CacheKey   string    `json:"cache_key" gorm:"column:cache_key;type:varchar(256);not null;uniqueIndex:idx_cache_key_field"`
	CacheField string    `json:"cache_field" gorm:"column:cache_field;type:varchar(256);not null;uniqueIndex:idx_cache_key_field"`
	CacheValue string    `json:"cache_value" gorm:"column:cache_value;type:blob;not null"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Message struct {
	ID        int64     `json:"id" gorm:"column:id;primaryKey;type:bigint(20) auto_increment"`
	Channel   string    `json:"channel" gorm:"column:channel;type:varchar(1024);not null;index"`
	Message   string    `json:"message" gorm:"column:message;type:text;not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MessageSubscribe struct {
	Channel       string    `json:"channel" gorm:"column:channel;type:varchar(1024);not null;uniqueIndex:idx_channel_subscriber"`
	Subscriber    string    `json:"subscriber" gorm:"column:subscriber;type:varchar(1024);not null;uniqueIndex:idx_channel_subscriber"`
	LastMessageId int64     `json:"last_message_id" gorm:"column:last_message_id;type:bigint(20);not null;default:-1"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

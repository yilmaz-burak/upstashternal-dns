package redis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DNSRecord represents a DNS record in Redis
type DNSRecord struct {
	IPs       []string          `json:"ips"`
	TTL       int               `json:"ttl"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// RedisClient handles Redis operations for DNS records
type RedisClient struct {
	rdb *redis.Client
}

// Client interface
type Client interface {
	SetRecord(ctx context.Context, hostname string, record *DNSRecord) error
	GetRecord(ctx context.Context, hostname string) (*DNSRecord, error)
	DeleteRecord(ctx context.Context, hostname string) error
}

// Option configures the Redis client
type Option func(*redis.Options)

// WithTLS enables TLS for Redis connection
func WithTLS(enabled bool) Option {
	return func(opts *redis.Options) {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
}

// NewClient creates a new Redis client
func NewClient(addr, password string, options ...Option) (Client, error) {
	opts := &redis.Options{
		Addr:      addr,
		Password:  password,
		TLSConfig: &tls.Config{},
	}

	for _, opt := range options {
		opt(opts)
	}

	rdb := redis.NewClient(opts)

	// Test connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %v", err)
	}

	return &RedisClient{rdb: rdb}, nil
}

// SetRecord sets a DNS record in Redis
func (c *RedisClient) SetRecord(ctx context.Context, hostname string, record *DNSRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %v", err)
	}

	key := fmt.Sprintf("dns:%s", hostname)
	if err := c.rdb.Set(ctx, key, string(data), time.Duration(record.TTL)*time.Second).Err(); err != nil {
		return fmt.Errorf("failed to set record: %v", err)
	}

	key = fmt.Sprintf("%s.", key)
	return c.rdb.Set(ctx, key, string(data), time.Duration(record.TTL)*time.Second).Err()
}

// GetRecord gets a DNS record from Redis
func (c *RedisClient) GetRecord(ctx context.Context, hostname string) (*DNSRecord, error) {
	key := fmt.Sprintf("dns:%s", hostname)
	data, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var record DNSRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal record: %v", err)
	}

	return &record, nil
}

// DeleteRecord deletes a DNS record from Redis
func (c *RedisClient) DeleteRecord(ctx context.Context, hostname string) error {
	key := fmt.Sprintf("dns:%s", hostname)
	err := c.rdb.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete record: %v", err)
	}

	key = fmt.Sprintf("%s.", key)
	return c.rdb.Del(ctx, key).Err()
}

package coredns

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/joho/godotenv"
	"github.com/miekg/dns"
	"github.com/redis/go-redis/v9"
	"k8s.io/klog/v2"
)

type Redis struct {
	Next          plugin.Handler
	RedisAddress  string
	RedisPassword string
	TTL           uint32
	client        *redis.Client
}

type RedisRecord struct {
	IPs      []string       `json:"ips"`
	TTL      int            `json:"ttl"`
	Metadata RecordMetadata `json:"metadata"`
}

type RecordMetadata struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
}

func NewRedisInstance() *Redis {
	if err := godotenv.Load("../../.env.test"); err != nil {
		klog.Warningf("Warning: .env.test file not found, using environment variables")
	}

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		klog.Fatal("REDIS_ADDR environment variable is required")
	}

	password := os.Getenv("REDIS_PASSWORD")
	if password == "" {
		klog.Fatal("REDIS_PASSWORD environment variable is required")
	}

	r := &Redis{
		RedisAddress:  addr,
		RedisPassword: password,
		TTL:           3600,
	}

	// Initialize Redis connection
	r.client = redis.NewClient(&redis.Options{
		Addr:      r.RedisAddress,
		Password:  r.RedisPassword,
		TLSConfig: &tls.Config{},
		DB:        0,
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.client.Ping(ctx).Err(); err != nil {
		klog.Fatalf("Failed to connect to Redis: %v", err)
	}

	return r
}

func (r *Redis) ServeDNS(ctx context.Context, w dns.ResponseWriter, msg *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: msg}

	// Only handle A record queries
	if state.QType() != dns.TypeA {
		klog.V(2).Infof("Skipping non-A record query for %s", state.Name())
		return plugin.NextOrFailure(r.Name(), r.Next, ctx, w, msg)
	}

	qname := state.Name()
	records, err := r.queryRedis(qname)
	if err != nil {
		klog.Errorf("Error querying Redis for %s: %v", qname, err)
		return plugin.NextOrFailure(r.Name(), r.Next, ctx, w, msg)
	}

	if len(records) == 0 {
		klog.V(2).Infof("No records found for %s", qname)
		return plugin.NextOrFailure(r.Name(), r.Next, ctx, w, msg)
	}

	m := new(dns.Msg)
	m.SetReply(msg)
	m.Authoritative = true
	m.Answer = make([]dns.RR, 0, len(records))

	for _, record := range records {
		rr, err := dns.NewRR(record)
		if err != nil {
			klog.Errorf("Error creating DNS record for %s: %v", qname, err)
			continue
		}
		m.Answer = append(m.Answer, rr)
	}

	if len(m.Answer) == 0 {
		klog.Warningf("Failed to create any valid DNS records for %s", qname)
		return plugin.NextOrFailure(r.Name(), r.Next, ctx, w, msg)
	}

	klog.V(2).Infof("Returning %d answers for %s", len(m.Answer), qname)
	w.WriteMsg(m)
	return dns.RcodeSuccess, nil
}

func (r *Redis) Name() string { return "upstashternal" }

func (r *Redis) queryRedis(qname string) ([]string, error) {
	klog.Infof("Querying Redis for %s", qname)
	key := fmt.Sprintf("dns:%s", qname)
	klog.Infof("Redis key: %s", key)

	val, err := r.client.Get(context.Background(), key).Result()
	if err != nil {
		if err == redis.Nil {
			klog.V(2).Infof("No DNS record found for %s", qname)
			return nil, nil
		}
		klog.Errorf("Redis query error for %s: %v", qname, err)
		return nil, fmt.Errorf("redis query error: %w", err)
	}

	var record RedisRecord
	if err := json.Unmarshal([]byte(val), &record); err != nil {
		klog.Errorf("Failed to parse Redis record for %s: %v", qname, err)
		return nil, fmt.Errorf("invalid record format: %w", err)
	}

	if len(record.IPs) == 0 {
		klog.V(2).Infof("No IPs found in record for %s", qname)
		return nil, nil
	}

	var records []string
	for _, ip := range record.IPs {
		records = append(records, fmt.Sprintf("%s %d IN A %s", qname, record.TTL, ip))
	}

	klog.V(2).Infof("Found %d IPs for %s", len(records), qname)
	return records, nil
}

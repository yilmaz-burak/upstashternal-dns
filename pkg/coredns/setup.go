package coredns

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() {
	plugin.Register("upstashternal", setup)
}

func setup(c *caddy.Controller) error {
	redis := NewRedisInstance()

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		redis.Next = next
		return redis
	})

	// Add cleanup on shutdown
	c.OnShutdown(func() error {
		return redis.client.Close()
	})

	return nil
}

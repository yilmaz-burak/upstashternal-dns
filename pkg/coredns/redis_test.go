package coredns

import (
	"context"
	"fmt"
	"testing"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
)

func TestRedis(t *testing.T) {
	redis := NewRedisInstance()

	tests := []struct {
		qname    string
		qtype    uint16
		expected int
		handler  plugin.Handler // Add custom next handler for specific tests
	}{
		{
			qname:    "test.upstashternal-dns.com.",
			qtype:    dns.TypeA,
			expected: dns.RcodeSuccess,
			handler:  test.NextHandler(dns.RcodeSuccess, nil),
		},
		{
			qname:    "nonexistent.com.",
			qtype:    dns.TypeA,
			expected: dns.RcodeNameError,
			handler:  test.NextHandler(dns.RcodeNameError, nil),
		},
	}

	for _, tc := range tests {
		redis.Next = tc.handler // Set the next handler for this test case
		m := new(dns.Msg)
		m.SetQuestion(tc.qname, tc.qtype)

		rec := dnstest.NewRecorder(&test.ResponseWriter{})
		code, err := redis.ServeDNS(context.TODO(), rec, m)

		fmt.Println("code", code)

		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if code != tc.expected {
			t.Errorf("Expected rcode %d, got %d", tc.expected, code)
		}
	}
}

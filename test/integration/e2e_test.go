package integration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"
	"github.com/miekg/dns"
	"github.com/upstash/redis-external-dns/pkg/controller"
	"github.com/upstash/redis-external-dns/pkg/coredns"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func TestEndToEndFlow(t *testing.T) {
	// Create k8s client from kubeconfig
	home := homedir.HomeDir()
	kubeconfig := filepath.Join(home, ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("error creating k8s config: %v", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("error creating k8s client: %v", err)
	}

	controller := controller.NewController(client)
	go controller.Run(1, make(chan struct{}))

	// Clean up any existing resources first
	err = client.CoreV1().Services("default").Delete(context.Background(), "test-service", metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		t.Fatalf("error deleting service: %v", err)
	}

	// Wait for service to be deleted
	for i := 0; i < 10; i++ {
		_, err = client.CoreV1().Services("default").Get(context.Background(), "test-service", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Create a test service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			Annotations: map[string]string{
				"upstashternal-dns.alpha.kubernetes.io/enabled":  "true",
				"upstashternal-dns.alpha.kubernetes.io/hostname": "test-service.upstashternal-dns.com",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 80,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	_, err = client.CoreV1().Services("default").Create(context.Background(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Create endpoints for the service
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "192.168.1.123"},
					{IP: "192.168.1.4"},
				},
				Ports: []corev1.EndpointPort{
					{
						Port: 80,
					},
				},
			},
		},
	}

	// Clean up any existing endpoints first
	err = client.CoreV1().Endpoints("default").Delete(context.Background(), "test-service", metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		t.Fatalf("error deleting endpoints: %v", err)
	}

	_, err = client.CoreV1().Endpoints("default").Create(context.Background(), endpoints, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create endpoints: %v", err)
	}

	// Wait for controller to process
	time.Sleep(5 * time.Second)

	// Initialize CoreDNS Redis plugin with real Redis client
	redisPlugin := coredns.NewRedisInstance()

	// Create DNS query
	m := new(dns.Msg)
	m.SetQuestion("test-service.upstashternal-dns.com.", dns.TypeA)
	rec := dnstest.NewRecorder(&test.ResponseWriter{})

	// Let the plugin handle the Redis query with prefix
	code, err := redisPlugin.ServeDNS(context.Background(), rec, m)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if code != dns.RcodeSuccess {
		t.Errorf("Expected success rcode, got %v", code)
	}

	if rec.Msg == nil {
		t.Fatal("Expected non-nil message")
	}

	// Verify the response
	if len(rec.Msg.Answer) != 2 {
		t.Fatalf("Expected 2 answers, got %d", len(rec.Msg.Answer))
	}

	expectedIPs := map[string]bool{
		"192.168.1.123": false,
		"192.168.1.4":   false,
	}

	for _, ans := range rec.Msg.Answer {
		if a, ok := ans.(*dns.A); ok {
			if _, exists := expectedIPs[a.A.String()]; !exists {
				t.Errorf("Unexpected IP in answer: %v", a.A.String())
			}
			expectedIPs[a.A.String()] = true
		} else {
			t.Error("Expected A record in answer")
		}
	}

	// Verify all expected IPs were found
	for ip, found := range expectedIPs {
		if !found {
			t.Errorf("Expected IP %s was not found in the answers", ip)
		}
	}
}

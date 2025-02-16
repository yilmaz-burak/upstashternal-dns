package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSyncService(t *testing.T) {
	// Create a fake k8s client
	client := fake.NewSimpleClientset()

	// Create a test service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
			Annotations: map[string]string{
				annotationEnabled:  "true",
				annotationHostname: "test.upstashternal-dns.com",
			},
		},
	}
	_, err := client.CoreV1().Services("default").Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error creating service: %v", err)
	}

	// Create test endpoints
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "192.168.1.1"},
					{IP: "192.168.1.2"},
				},
			},
		},
	}
	_, err = client.CoreV1().Endpoints("default").Create(context.TODO(), endpoints, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error creating endpoints: %v", err)
	}

	// Create the controller
	c := NewController(client)

	// Test sync
	err = c.syncService("default/test-service")
	if err != nil {
		t.Errorf("syncService error: %v", err)
	}

	// Verify Redis record was created with correct IPs
	record, err := c.redis.GetRecord(context.TODO(), "test.upstashternal-dns.com")
	if err != nil {
		t.Errorf("error getting redis record: %v", err)
	}
	if len(record.IPs) != 2 {
		t.Errorf("expected 2 IPs, got %d", len(record.IPs))
	}
}

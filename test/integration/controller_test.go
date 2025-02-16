package integration

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/upstash/redis-external-dns/pkg/controller"
	"github.com/upstash/redis-external-dns/pkg/redis"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func init() {
	if err := godotenv.Load("../../.env.test"); err != nil {
		log.Printf("Warning: .env.test file not found")
	}
}

func TestControllerIntegration(t *testing.T) {
	// Skip if not in integration test environment
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	redisClient, err := redis.NewClient(
		os.Getenv("REDIS_ADDR"),
		os.Getenv("REDIS_PASSWORD"),
	)
	if err != nil {
		t.Fatalf("error creating redis client: %v", err)
	}

	// Create k8s client
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback to kubeconfig
		home := homedir.HomeDir()
		kubeconfig := filepath.Join(home, ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			t.Fatalf("error creating k8s config: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("error creating k8s client: %v", err)
	}

	// Clean up and wait for resources to be deleted
	err = clientset.CoreV1().Services("default").Delete(context.TODO(), "integration-test", metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		t.Fatalf("error deleting service: %v", err)
	}

	err = clientset.CoreV1().Endpoints("default").Delete(context.TODO(), "integration-test", metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		t.Fatalf("error deleting existing endpoints: %v", err)
	}

	// Wait for resources to be deleted with retries
	for i := 0; i < 10; i++ {
		_, err = clientset.CoreV1().Endpoints("default").Get(context.TODO(), "integration-test", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Create and start controller
	c := controller.NewController(clientset)
	stopCh := make(chan struct{})
	go func() {
		if err := c.Run(1, stopCh); err != nil {
			t.Errorf("controller error: %v", err)
		}
	}()

	// Create test service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "integration-test",
			Namespace: "default",
			Annotations: map[string]string{
				"upstashternal-dns.alpha.kubernetes.io/enabled":  "true",
				"upstashternal-dns.alpha.kubernetes.io/hostname": "test2.upstashternal-dns.com",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app": "integration-test",
			},
		},
	}

	_, err = clientset.CoreV1().Services("default").Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("error creating service: %v", err)
	}

	// Create or update endpoints
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "integration-test",
			Namespace: "default",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "192.168.1.11"},
					{IP: "192.168.1.22"},
				},
				Ports: []corev1.EndpointPort{
					{
						Port: 8080,
					},
				},
			},
		},
	}

	_, err = clientset.CoreV1().Endpoints("default").Create(context.TODO(), endpoints, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = clientset.CoreV1().Endpoints("default").Update(context.TODO(), endpoints, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("error updating endpoints: %v", err)
			}
		} else {
			t.Fatalf("error creating endpoints: %v", err)
		}
	}

	// Wait for controller to process
	time.Sleep(5 * time.Second)

	// Verify Redis record
	record, err := redisClient.GetRecord(context.TODO(), "test2.upstashternal-dns.com")
	if err != nil {
		t.Fatalf("error getting record: %v", err)
	}

	if record == nil {
		t.Error("expected record to exist")
	}
}

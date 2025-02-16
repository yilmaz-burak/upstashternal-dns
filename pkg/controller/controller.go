package controller

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	redisClient "github.com/upstash/redis-external-dns/pkg/redis"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	// The annotation key to enable DNS record creation
	annotationEnabled = "upstashternal-dns.alpha.kubernetes.io/enabled"
	// The annotation key for the hostname
	annotationHostname = "upstashternal-dns.alpha.kubernetes.io/hostname"
)

// Controller watches Kubernetes Services and updates Redis DNS records
type Controller struct {
	client    kubernetes.Interface
	informer  cache.SharedIndexInformer
	queue     workqueue.RateLimitingInterface
	namespace string
	redis     redisClient.Client
	stopCh    chan struct{}
}

// NewController creates a new DNS controller
func NewController(client kubernetes.Interface) *Controller {
	if err := godotenv.Load("../../.env.test"); err != nil {
		log.Printf("Warning: .env.test file not found")
	}

	redisClient, err := redisClient.NewClient(os.Getenv("REDIS_ADDR"), os.Getenv("REDIS_PASSWORD"))
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}

	c := &Controller{
		client:    client,
		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		namespace: metav1.NamespaceAll,
		redis:     redisClient,
		stopCh:    make(chan struct{}),
	}

	// Create the service informer
	c.informer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Services(c.namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Services(c.namespace).Watch(context.TODO(), options)
			},
		},
		&corev1.Service{},
		0, // Skip resync
		cache.Indexers{},
	)

	// Add event handlers
	c.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handleAdd,
		UpdateFunc: c.handleUpdate,
		DeleteFunc: c.handleDelete,
	})

	return c
}

// Run starts the controller
func (c *Controller) Run(workers int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Info("Starting Service controller")

	// Start the informer
	go c.informer.Run(stopCh)

	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.informer.HasSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	// Add periodic reconciliation
	go wait.Until(c.reconcileAllServices, 5*time.Second, stopCh)

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

func (c *Controller) runWorker() {
	klog.Info("Running worker")
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	// Get next item from queue
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	// Process the item
	err := c.syncService(key.(string))
	if err != nil {
		klog.Errorf("Error syncing service %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(key)
	return true
}

// Event handlers
func (c *Controller) handleAdd(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Printf("Error getting key for object: %v", err)
		return
	}
	c.queue.Add(key)
}

func (c *Controller) handleUpdate(oldObj, newObj interface{}) {
	c.handleAdd(newObj)
}

func (c *Controller) handleDelete(obj interface{}) {
	// Get the service before it was deleted
	service, ok := obj.(*corev1.Service)
	if !ok {
		// In case of a DeletedFinalStateUnknown, try to extract the service
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object, invalid type")
			return
		}
		service, ok = tombstone.Obj.(*corev1.Service)
		if !ok {
			klog.Errorf("Error decoding object tombstone, invalid type")
			return
		}
	}

	// Check if this service had our annotations
	if enabled, ok := service.Annotations[annotationEnabled]; !ok || enabled != "true" {
		return
	}

	hostname, ok := service.Annotations[annotationHostname]
	if !ok || hostname == "" {
		return
	}

	// Delete the DNS record from Redis
	if err := c.redis.DeleteRecord(context.TODO(), hostname); err != nil {
		klog.Errorf("Error deleting DNS record for %s: %v", hostname, err)
		return
	}

	klog.Infof("Deleted DNS record for %s due to service deletion", hostname)
}

// syncService processes a service and updates Redis DNS records
func (c *Controller) syncService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %s", key)
	}

	// Get the Service resource
	service, err := c.client.CoreV1().Services(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error fetching service %s/%s: %v", namespace, name, err)
	}

	// Check if service has our annotation
	if enabled, ok := service.Annotations[annotationEnabled]; !ok || enabled != "true" {
		return nil
	}

	hostname, ok := service.Annotations[annotationHostname]
	if !ok || hostname == "" {
		return fmt.Errorf("hostname annotation missing for service %s/%s", namespace, name)
	}

	// Get service endpoints
	endpoints, err := c.client.CoreV1().Endpoints(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error fetching endpoints for service %s/%s: %v", namespace, name, err)
	}

	// Collect pod IPs
	var ips []string
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			ips = append(ips, addr.IP)
		}
	}

	// Update Redis record
	record := &redisClient.DNSRecord{
		IPs:       ips,
		TTL:       10, // TODO: Make configurable
		UpdatedAt: time.Now(),
		Metadata: map[string]string{
			"namespace": namespace,
			"service":   name,
		},
	}

	if err := c.redis.SetRecord(context.TODO(), hostname, record); err != nil {
		return fmt.Errorf("error updating Redis record: %v", err)
	}

	klog.Infof("Updated DNS record for %s with IPs: %v", hostname, ips)
	return nil
}

// Add new method to reconcile all services
func (c *Controller) reconcileAllServices() {
	services, err := c.client.CoreV1().Services(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("Error listing services: %v", err)
		return
	}

	for _, svc := range services.Items {
		// Check if service has our annotation
		if enabled, ok := svc.Annotations[annotationEnabled]; !ok || enabled != "true" {
			continue
		}

		// Check if hostname annotation exists
		if _, ok := svc.Annotations[annotationHostname]; !ok {
			continue
		}
		// Enqueue service for processing
		c.queue.Add(fmt.Sprintf("%s/%s", svc.Namespace, svc.Name))
	}
}

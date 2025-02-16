package main

import (
	"log"

	"github.com/upstash/redis-external-dns/pkg/controller"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	var config *rest.Config
	var err error

	// Try in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		log.Fatalf("ERROR: %s", err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	// Create and start controller
	c := controller.NewController(clientset)
	stopCh := make(chan struct{})
	if err := c.Run(1, stopCh); err != nil {
		log.Fatal(err)
	}
}

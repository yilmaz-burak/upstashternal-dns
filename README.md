# Upstash Redis-based DNS Controller and CoreDNS Plugin for Multi-Cluster Service Discovery

A comprehensive DNS solution for Kubernetes that combines a custom External DNS controller and CoreDNS plugin, using Upstash Redis for cross-cluster service discovery.

## Overview

This system provides a complete DNS solution for Kubernetes clusters by combining two main components:

1. A custom External DNS controller that syncs Kubernetes Services to Redis
2. A custom CoreDNS plugin that resolves DNS queries using the Redis records

This enables:
- Cross-cluster service discovery
- Custom DNS resolution logic
- Centralized DNS record management
- Real-time service updates
- Consistent DNS resolution across multiple clusters

## Architecture

### Components

1. **DNS Controller (External DNS)**
   - Watches Kubernetes Services across clusters
   - Syncs service endpoints to Upstash Redis
   - Supports flexible configuration via annotations:
     - `upstashternal-dns.alpha.kubernetes.io/enabled: "true"`
     - `upstashternal-dns.alpha.kubernetes.io/hostname: "your.hostname.com"`

3. **Upstash Redis Backend**
   - Acts as the central source of truth
   - Stores DNS records with TTL
   - Key format: `dns:{hostname}`
   - Value format: JSON containing IPs and metadata

3. **CoreDNS Plugin**
   - Custom plugin for Upstash Redis integration
   - Resolves DNS queries using Upstash Redis records
   - Supports TTL and caching

### Flow

1. Service Creation:
   ```
   Service Created → DNS Controller → Upstash Redis Update → DNS Record Available
   ```

2. DNS Resolution:
   ```
   Client Query → CoreDNS → Upstash Redis Lookup → IP Address Return
   ```

## Development

### Prerequisites
- Go 1.22+
- Docker
- Kubernetes cluster
- Upstash Redis instance

### Environment Variables
Required environment variables:
- `REDIS_ADDR`: Upstash Redis server address
- `REDIS_PASSWORD`: Upstash Redis password

### Quick Start

1. Start Minikube:
```bash
minikube start
```

2. Configure Docker environment:
```bash
eval $(minikube docker-env)
```

3. Build components:
```bash
# Build DNS controller
docker build -t upstashternal-dns-controller:latest .

# Build custom CoreDNS
docker build -t upstashternal-coredns:latest -f Dockerfile.coredns .
```

4. Deploy:
```bash
# Deploy controller and CoreDNS
kubectl apply -f deploy/

# Deploy test service
kubectl apply -f examples/test-service.yaml
```

### Testing

1. Verify deployment:
```bash
kubectl get pods -l app=upstashternal-dns
kubectl get pods -n kube-system -l app=upstashternal-coredns
```

2. Monitor logs:
```bash
# Controller logs
kubectl logs -l app=upstashternal-dns

# CoreDNS logs
kubectl logs -n kube-system -l app=upstashternal-coredns
```

3. Test DNS resolution:
```bash
# Start a debug pod
kubectl run -it --rm debug --image=alpine --restart=Never -- sh

# Install dig
apk add bind-tools

# Test DNS resolution
dig @upstashternal-coredns.kube-system.svc.cluster.local test-service.upstashternal-dns.com
```

## Contributing

This project welcomes contributions and suggestions. Feel free to submit issues and pull requests.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
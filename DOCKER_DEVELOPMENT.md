# Docker Development Guide

This guide covers local Docker development for the IPMI exporter using the multi-stage Dockerfile.

## Files Overview

### Dockerfile.local
Multi-stage Dockerfile for local development and testing:
- **Stage 1**: Builds the Go binary using golang:1.24-alpine
- **Stage 2**: Creates runtime image based on Alpine Linux
- **Features**: Static binary, minimal runtime, health checks

### docker-compose.local.yml
Docker Compose configuration for local development:
- **ipmi-exporter-local**: Main exporter service
- **ipmi-exporter-test**: Test mode execution
- **prometheus**: Optional Prometheus for scraping metrics

### prometheus.yml
Basic Prometheus configuration for local testing.

## Quick Start

### 1. Build Local Image
```bash
# Build using Makefile
make docker-local

# Or build directly
docker build -f Dockerfile.local -t ipmi-exporter:local .
```

### 2. Run Test Mode
```bash
# Run test mode in Docker
make docker-test

# Run test mode with debug output
make docker-test-debug

# Or run directly
docker run --rm --privileged \
  -v $(PWD)/ipmi_local.yml:/etc/ipmi-exporter/ipmi-local.yml:ro \
  ipmi-exporter:local --test

# Run with debug
docker run --rm --privileged \
  -v $(PWD)/ipmi_local.yml:/etc/ipmi-exporter/ipmi-local.yml:ro \
  ipmi-exporter:local --test --test.debug
```

### 3. Start Development Environment
```bash
# Start all services
make docker-compose-up

# Or with docker-compose
docker-compose -f docker-compose.local.yml up -d
```

### 4. Access Services
- **IPMI Exporter**: http://localhost:9290
- **Prometheus**: http://localhost:9090
- **IPMI Exporter Metrics**: http://localhost:9290/metrics

### 5. Stop Environment
```bash
# Stop all services
make docker-compose-down

# Or with docker-compose
docker-compose -f docker-compose.local.yml down
```

## Development Workflow

### Local Testing
```bash
# 1. Build image
make docker-local

# 2. Run tests
make docker-test

# 3. Start services
make docker-compose-up

# 4. Check logs
make docker-compose-logs

# 5. Test changes
docker-compose -f docker-compose.local.yml restart ipmi-exporter-local
```

### Configuration Changes
```bash
# Edit configuration files
vim ipmi_local.yml

# Restart service
docker-compose -f docker-compose.local.yml restart ipmi-exporter-local

# Or rebuild and restart
make docker-local
docker-compose -f docker-compose.local.yml up -d --force-recreate ipmi-exporter-local
```

### Debug Mode
```bash
# Run with debug logging
docker run --rm --privileged \
  -v $(PWD)/ipmi_local.yml:/etc/ipmi-exporter/ipmi-local.yml:ro \
  ipmi-exporter:local --log.level=debug

# Get shell in container
docker run --rm --privileged -it \
  --entrypoint /bin/sh \
  ipmi-exporter:local
```

## Dockerfile.local Details

### Build Stage
```dockerfile
FROM golang:1.24-alpine AS builder
# - Downloads Go modules
# - Builds static binary
# - Optimizes for size and security
```

### Runtime Stage
```dockerfile
FROM alpine:3
# - Installs FreeIPMI tools
# - Copies binary from build stage
# - Sets up non-root user
# - Includes health checks
```

### Key Features
- **Static Binary**: No external dependencies at runtime
- **Minimal Image**: Based on Alpine Linux (~15MB)
- **Security**: Runs as non-root user
- **Health Checks**: Built-in container health monitoring
- **Configuration**: Mountable config files

## Docker Compose Services

### ipmi-exporter-local
```yaml
services:
  ipmi-exporter-local:
    build:
      dockerfile: Dockerfile.local
    ports:
      - "9290:9290"
    privileged: true  # Required for IPMI access
```

### ipmi-exporter-test
```yaml
  ipmi-exporter-test:
    command: ["--test"]
    restart: "no"  # Run once and exit
```

### prometheus (optional)
```yaml
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    depends_on:
      - ipmi-exporter-local
```

## Makefile Targets

### Docker Commands
```bash
make docker-local          # Build local image
make docker-test           # Run test mode
make docker-test-debug     # Run test mode with debug output
make docker-compose-up     # Start dev environment
make docker-compose-down   # Stop dev environment
make docker-compose-logs   # Show logs
```

### Integration with Existing Targets
```bash
make test-docker           # Test standard Dockerfile
make test-docker-buildx    # Test multi-arch build
make test-local            # Test GoReleaser config
```

## Troubleshooting

### Permission Issues
```bash
# IPMI requires privileged mode
docker run --privileged ...

# Or add capabilities
docker run --cap-add=CAP_SYS_RAWIO ...
```

### Build Failures
```bash
# Check Go version in Dockerfile
# Update if needed: FROM golang:1.24-alpine

# Clean build cache
docker builder prune -f
```

### Configuration Issues
```bash
# Check config file mounting
docker run --rm -it \
  -v $(PWD)/ipmi_local.yml:/etc/ipmi-exporter/ipmi-local.yml:ro \
  --entrypoint /bin/sh \
  ipmi-exporter:local
ls -la /etc/ipmi-exporter/
```

### Network Issues
```bash
# Check network connectivity
docker network ls
docker network inspect <network-name>

# Test IPMI connectivity
docker run --privileged --rm \
  ipmi-exporter:local ipmi-sensor --help
```

## Production vs Development

| Feature | Dockerfile | Dockerfile.local |
|----------|------------|------------------|
| Use Case | Production | Development |
| Build | Single-stage | Multi-stage |
| Binary | Pre-built | Built from source |
| Size | Optimized | Larger (includes build tools) |
| Speed | Fast | Slower (compilation) |
| Debugging | Limited | Full source access |

## Best Practices

### Development
1. Use `Dockerfile.local` for feature development
2. Run tests before building production image
3. Mount configuration files for easy changes
4. Use `--privileged` mode for IPMI access

### Before Production
1. Test with production Dockerfile
2. Verify configuration
3. Check security settings
4. Validate with GoReleaser

### Performance
1. Use Docker build cache
2. Minimize context size
3. Use .dockerignore if needed
4. Consider multi-stage optimizations

## Integration with CI/CD

The local Docker setup integrates with existing CI/CD:

```bash
# Test locally before CI
make docker-local
make docker-test

# Same tests run in CI
make test-docker
make test-local
```

This ensures consistency between local development and CI/CD pipeline.

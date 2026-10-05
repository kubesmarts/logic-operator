# Workflow Gateway

The workflow gateway is a stateless HTTP routing layer that directs requests to appropriate workflow runtime services. It implements the routing strategy defined in [ADR-0002: Workflow Gateway Architecture](https://github.com/kubesmarts/logic-apps/blob/main/adrs/0002-workflow-gateway-architecture.md).

## Building

Build the gateway binary from the project root:

```bash
make build-gateway
```

The binary is created at `bin/workflow-gateway`.

## Running Locally

Start the gateway from the project root:

```bash
./bin/workflow-gateway
```

The server listens on `:8080` and exposes health and readiness probes:

```bash
curl http://localhost:8080/health    # Liveness probe
curl http://localhost:8080/ready     # Readiness probe
```

## Configuration

The gateway accepts configuration via `server.Config`. Currently supported configuration:

| Field | Default | Description |
|-------|---------|-------------|
| Addr | `:8080` | HTTP server bind address |
| ReadTimeout | `15s` | HTTP request read timeout |
| WriteTimeout | `15s` | HTTP response write timeout |
| IdleTimeout | `60s` | HTTP connection idle timeout |
| ShutdownTimeout | `30s` | Graceful shutdown timeout |

Configuration can be overridden in `cmd/main.go` by modifying the `server.Config` struct.

## Testing

Run unit tests:

```bash
cd workflow-gateway
go test -v ./...
```

Run with coverage:

```bash
cd workflow-gateway
go test -v -cover ./...
```

Current coverage: 86.4% of statements

## Docker

### Building

Build Docker image from project root:

```bash
make docker-build-gateway
```

The image is built with tag `kubesmarts.org/workflow-gateway:v0.0.1` (uses VERSION variable).

### Running

Run the container:

```bash
docker run -p 8080:8080 kubesmarts.org/workflow-gateway:v0.0.1
```

The container runs as non-root user (uid 65532) for security.

### Multi-Platform Builds

The Dockerfile supports multi-platform builds:

```bash
docker buildx build --platform linux/amd64,linux/arm64 \
  -t kubesmarts.org/workflow-gateway:latest .
```

## Endpoints

### GET /health

Liveness probe. Always returns 200 OK.

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

**Use case:** Kubernetes liveness probe to detect if the server is alive.

### GET /ready

Readiness probe. Returns 200 OK when ready, 503 Service Unavailable when not ready.

```bash
curl http://localhost:8080/ready
# {"status":"ready"} (200 OK) or {"status":"not ready"} (503)
```

**Use case:** Kubernetes readiness probe to detect if the server is ready to accept traffic. Returns 503 until initialization is complete.

## Architecture

See [ADR-0002: Workflow Gateway Architecture](https://github.com/kubesmarts/logic-apps/blob/main/adrs/0002-workflow-gateway-architecture.md) for the complete architecture and routing strategy.

This phase (Phase 2 of ADR-0002) establishes the HTTP server scaffold. Future phases will add:
- Workflow instance routing (two-level routing strategy)
- CRD watching for LogicFlowRuntime discovery
- Data-Index integration for instance status queries
- Header-based pod routing via sticky sessions

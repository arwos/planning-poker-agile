# Docker deployment

## Start with Docker Compose

Run from the repository root:

```bash
docker compose --file docker/docker-compose.yml up --build -d
```

Open <http://localhost:8080>. For production, put Nginx or another TLS reverse proxy in front of the container; do not expose the container port directly to the Internet. Stop the service with:

```bash
docker compose --file docker/docker-compose.yml down
```

The image builds Angular, embeds its hashed assets into the Go binary, and exposes port `8080`. Compose marks the container healthy after `/healthz` responds successfully.

## Configuration with environment variables

Docker Compose provides defaults. Override one or more values inline or through a `.env` file in the repository root.

```bash
HOST_PORT=8090 ROOMS_MAX_CONCURRENT=25 docker compose --file docker/docker-compose.yml up --build
```

| Variable | Default                 | Meaning |
| --- |-------------------------| --- |
| `HOST_PORT` | `8080`                  | Host port mapped to container port 8080. |
| `DOCKER_BIND_ADDRESS` | `127.0.0.1` | Host address on which the container port is published. Keep the default when using Nginx on the same host. |
| `IMAGE` | `planning-poker`        | Compose image name. |
| `TAG` | `latest`                | Compose image tag. |
| `ROOMS_MAX_CONCURRENT` | `100`                   | Maximum active in-memory rooms; `0` disables limit. |
| `ROOMS_MAX_ROLES` | `10`                    | Maximum roles in a newly created room; `0` disables limit. |
| `ROOMS_MAX_STORY_POINTS` | `10`                    | Maximum story points in a newly created room; `0` disables limit. |
| `ROOMS_MAX_PARTICIPANTS` | `32`                    | Maximum participants in one room. |
| `ROOMS_PENDING_TTL` | `10m`                    | How long an unjoined room invitation remains available. |
| `WEBSOCKET_PING_INTERVAL` | `1s`                    | Ping/pong interval as a Go duration. |
| `WEBSOCKET_MAX_MESSAGE_BYTES` | `32768`                 | Maximum inbound WebSocket message size. |
| `WEBSOCKET_MAX_CONNECTIONS` | `1000`                 | Maximum pre-join and active WebSocket connections in one process. |
| `WEBSOCKET_JOIN_TIMEOUT` | `10s`                 | Maximum time allowed for the first `join` frame. |
| `WEBSOCKET_MESSAGE_RATE_PER_SECOND` | `20`                 | Per-connection inbound message rate. |
| `WEBSOCKET_MESSAGE_BURST` | `40`                 | Initial inbound message burst. |
| `WEBSOCKET_OUTBOUND_QUEUE_SIZE` | `32`                 | Per-client outbound event queue. Slow clients are disconnected when full. |
| `WEBSOCKET_WRITE_TIMEOUT` | `5s`                 | Maximum duration of one outbound write or ping. |
| `HTTP_CREATE_RATE_PER_MINUTE` | `10`                 | Room creation rate per client IP. |
| `CORS_ORIGINS` | `*`                     | Comma-separated browser origins allowed for cross-origin API/WebSocket access; `*` allows all origins. |

The binary reads YAML configuration first, then applies `PLANNING_POKER_*` environment variables. The compose file maps the variables above to that prefix. To override all configuration from a custom YAML file, mount it at `/app/config/config.yaml` or mount another path and set `CONFIG_PATH`.

The default `*` allows browser requests from any origin. Set `CORS_ORIGINS` to an explicit comma-separated list in production.

## Nginx with HTTPS and WebSocket

[`nginx/planning-poker.conf.example`](nginx/planning-poker.conf.example) is a host-level reverse-proxy template. It redirects HTTP to HTTPS, forwards WebSocket upgrade/ping traffic, limits room creation and per-IP WebSocket connections, and sets TLS/security headers.

Before enabling it:

1. Point `server_name` at the real hostname and provision a certificate, for example with Certbot.
2. Replace the certificate paths in the template.
3. Keep `DOCKER_BIND_ADDRESS=127.0.0.1` so clients cannot bypass Nginx and the TLS policy.
4. Validate and reload Nginx:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

The template assumes the application is published on `127.0.0.1:8080`. It overwrites `X-Forwarded-For`, and the backend accepts forwarded client IPs only from a loopback peer, so the per-IP creation limiter continues to work without trusting spoofed headers. If Nginx runs in Docker on the same Compose network, change the upstream to `planning-poker:8080` and use the Docker network instead of the host-published port. TLS termination remains the reverse proxy's responsibility; the Go service should not be advertised as HTTPS when it is serving plaintext HTTP internally.

## Docker Hub

```bash
docker login
make docker-build IMAGE=your-dockerhub-user/planning-poker TAG=v1.0.0
make docker-push IMAGE=your-dockerhub-user/planning-poker TAG=v1.0.0
```

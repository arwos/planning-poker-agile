# Docker deployment

## Start with Docker Compose

Run from the repository root:

```bash
docker compose --file docker/docker-compose.yml up --build -d
```

Open <http://localhost:8080>. Stop the service with:

```bash
docker compose --file docker/docker-compose.yml down
```

The image builds Angular, embeds its hashed assets into the Go binary, and exposes port `8080`.

## Configuration with environment variables

Docker Compose provides defaults. Override one or more values inline or through a `.env` file in the repository root.

```bash
HOST_PORT=8090 ROOMS_MAX_CONCURRENT=25 docker compose --file docker/docker-compose.yml up --build
```

| Variable | Default                 | Meaning |
| --- |-------------------------| --- |
| `HOST_PORT` | `8080`                  | Host port mapped to container port 8080. |
| `IMAGE` | `planning-poker`        | Compose image name. |
| `TAG` | `latest`                | Compose image tag. |
| `ROOMS_MAX_CONCURRENT` | `100`                   | Maximum active in-memory rooms; `0` disables limit. |
| `ROOMS_MAX_ROLES` | `10`                    | Maximum roles in a newly created room; `0` disables limit. |
| `ROOMS_MAX_STORY_POINTS` | `10`                    | Maximum story points in a newly created room; `0` disables limit. |
| `WEBSOCKET_PING_INTERVAL` | `1s`                    | Ping/pong interval as a Go duration. |
| `WEBSOCKET_MAX_MESSAGE_BYTES` | `32768`                 | Maximum inbound WebSocket message size. |
| `CORS_ORIGINS` | `http://localhost:8080` | Comma-separated browser origins allowed for cross-origin API/WebSocket access. |

The binary reads YAML configuration first, then applies `PLANNING_POKER_*` environment variables. The compose file maps the variables above to that prefix. To override all configuration from a custom YAML file, mount it at `/app/config/config.yaml` or mount another path and set `CONFIG_PATH`.

## Docker Hub

```bash
docker login
make docker-build IMAGE=your-dockerhub-user/planning-poker TAG=v1.0.0
make docker-push IMAGE=your-dockerhub-user/planning-poker TAG=v1.0.0
```

# Planning Poker Agile

Real-time Planning Poker for Agile teams. Create a private room, invite teammates, vote with story points, and reveal a shared estimate when every voter is ready.

## Features

- Private in-memory rooms addressed by UUID links.
- Custom story point and role sets, with configurable limits.
- Real-time WebSocket presence, voting, automatic result reveal, role averages, and lead-controlled reset.
- Responsive retro-futuristic Angular interface with embedded production assets.
- Rooms disappear automatically when their final participant disconnects.

## Architecture

| Area | Technology |
| --- | --- |
| Backend | Go 1.26, `github.com/coder/websocket` |
| Frontend | Angular 22, pnpm |
| Transport | HTTP API + WebSocket |
| Storage | Process memory only |
| Delivery | Single Go binary with embedded hashed frontend assets |

## Docker Image

```bash
docker pull markus621/planning-poker-agile:latest
```

## Quick start

Prerequisites: Go 1.26, Node.js 24+, pnpm 11+, and GNU Make.

```bash
make install
make build
./backend/server
```

Open <http://localhost:8080>. The health endpoint is <http://localhost:8080/healthz>.

For frontend development with live reload, run the backend and frontend separately:

```bash
make run-backend
make run-frontend
```

The Angular dev server runs on <http://localhost:4200> and connects to the backend on port `8080`.

## Configuration

The default config is [`backend/config/config.yaml`](backend/config/config.yaml). Use `CONFIG_PATH` to select another YAML file.

```yaml
http:
  host: "0.0.0.0"
  port: 8080
  read_header_timeout: 5s
  write_timeout: 10s
  create_rate_per_minute: 10
rooms:
  max_concurrent: 100
  max_roles: 10
  max_story_points: 10
  max_participants: 32
  pending_ttl: 10m
websocket:
  ping_interval: 1s
  max_message_bytes: 32768
  max_connections: 1000
  join_timeout: 10s
  message_rate_per_second: 20
  message_burst: 40
  outbound_queue_size: 32
  write_timeout: 5s
cors_origins: ["*"]
```

The default `*` allows browser requests from any origin. Override `cors_origins` with an explicit list in production.

Role and story point limits may be set to `0` to disable those individual limits. The concurrent room, participant, and WebSocket connection limits always retain a safe positive default.

## API

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/api/rooms` | Creates a room from `{ "cards": [...], "roles": [...] }`; the response includes a private `owner_token` for the creator. |
| `GET` | `/api/rooms/{id}` | Returns public room state. |
| `GET` | `/ws/rooms/{id}` | Opens a room WebSocket; first message must be `join` with `name`, `role`, and a stable private `client_id`; reconnects additionally send the server-issued `reconnect_token`; the creator also sends `owner_token`. |
| `GET` | `/healthz` | Liveness endpoint. |

WebSocket client events include `join`, `vote_selected`, `vote_submitted`, `vote_skipped`, and `reset`. `vote_skipped` marks a voter as submitted without adding a value to the results.

Only selected votes are private. Submitted votes become visible after the server reveals results. Room pages send `X-Robots-Tag: noindex` and are excluded from `robots.txt`. Configure TLS at a reverse proxy before exposing room links or owner tokens outside a trusted network.

## Development commands

```bash
make install          # install Go and pnpm dependencies
make lint             # run backend and frontend linters
make test             # run test suites
make build            # build embedded frontend and backend binary
make run-backend      # run backend locally
make run-frontend     # run Angular dev server
```

Backend linting needs a current `golangci-lint` release built with Go 1.26 or newer.

## Docker

The Docker image is multi-stage: it builds Angular, embeds the output into the Go binary, then creates a minimal Alpine runtime image.

```bash
make docker-build IMAGE=your-dockerhub-user/planning-poker TAG=v1.0.0
make docker-run IMAGE=your-dockerhub-user/planning-poker TAG=v1.0.0
make docker-push IMAGE=your-dockerhub-user/planning-poker TAG=v1.0.0
```

For local container orchestration, use `make docker-up` and `make docker-down`. See [`docker/README.md`](docker/README.md) for environment-variable configuration.

Authenticate with `docker login` before pushing. The container exposes port `8080`; mount a custom YAML file and set `CONFIG_PATH` if configuration needs changing.

## Verification

```bash
cd backend && GOCACHE=/tmp/planningpoker-go-cache go test ./...
cd backend && GOCACHE=/tmp/planningpoker-go-cache go vet ./...
cd frontend && pnpm lint && pnpm build
```

## License

This project is licensed under the [GNU General Public License v3.0](LICENSE).

Copyright (c) 2026 Mikhail Knyazhev.

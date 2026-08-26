# Contributor and agent guide

## Project goals and invariants

Planning Poker is a single-process, real-time estimation service. A room exists only while at least one WebSocket participant is connected. Do not add persistent storage, queues, Redis, session databases, user accounts, or cross-process coordination unless the task explicitly requests an architectural change.

The server is authoritative for rooms, participants, voting state, completion, reveal, reset, and access to lead actions. The client may retain input fields and an unsubmitted card only; it must accept each server state update as the source of truth.

## Repository map

```text
backend/
  app/room/       Domain entities, validation, registry, voting lifecycle
  app/voting/     Pure vote calculations
  app/realtime/   Room-scoped event fan-out
  pkg/config/     YAML parsing and configuration validation
  pkg/httpapi/    REST, WebSocket upgrade, embedded SPA delivery
  pkg/ws/         Serialized WebSocket writes and ping lifecycle
  cmd/server/     Dependency wiring only
frontend/
  src/app/models/    Shared client shapes
  src/app/services/  HTTP, WebSocket, browser integrations
  src/app/           Angular component `.ts`, `.html`, `.css` triplets
docker/             Dockerfile for the deployable image
```

`cmd/server/main.go` must remain composition-only. Keep business decisions in `app`, infrastructure and transport concerns in `pkg`.

## Room and voting rules

- A room UUID is a shareable private link; do not log or transform it into sequential IDs.
- First connected participant is lead. Transfer lead to an active participant when the lead leaves.
- A participant without a role is an observer and never blocks reveal.
- Do not reveal another participant's card before all active voters submit.
- Results consist of total average and role averages rounded to one decimal place.
- Only the lead may reset. A reset clears every submitted vote and client-side selected card.
- Delete a room from the registry after the last participant disconnects.
- Read limits, room capacity, role count, and story point count are configured limits; preserve validation at the server boundary.

## WebSocket protocol

Frames are JSON objects with `type`. Change backend handler, frontend handling, documentation, and tests together whenever changing this protocol.

Client events: `join`, `vote_selected`, `vote_submitted`, `reset`.

Server events: `room_state`, `participant_joined`, `participant_left`, `results_revealed`, `voting_reset`, `error`.

The first client event must be `join`. Errors use `{ "type": "error", "error": "safe message" }`. Never add raw internal errors, stack traces, or private vote values to public payloads.

## Concurrency and lifecycle

- Protect `Registry` and `Room` mutable state with their existing locks.
- Never write to a WebSocket while holding a room or registry lock.
- Use `realtime.Hub` for room broadcasts and `ws.Session` for serialized connection writes.
- Keep ping/pong behavior active. A failed ping must lead to connection closure and participant cleanup.
- Any new goroutine requires a clear cancellation path tied to the request or connection context.

## Frontend conventions

- Keep every Angular component in separate `.ts`, `.html`, and `.css` files.
- Keep API/browser behaviour in injectable services and reusable response shapes in `models/`.
- Use signals for local UI state. Do not duplicate server voting or participant state in ad-hoc stores.
- Preserve the responsive two-column room layout, role colors, keyboard controls, visible focus states, adequate contrast, and touch targets.
- Room settings and last chosen role intentionally use `localStorage`; namespace new browser keys with `planning-poker.`.
- The browser bundle has hashed filenames. Do not remove `outputHashing: "all"` or the backend static cache policy without a replacement cache-busting strategy.

## Embedded frontend and static files

Angular production output goes to `backend/pkg/httpapi/web` and is ignored by Git. The Go `embed.FS` bundle serves `/` and SPA routes, while API and WebSocket routes take precedence. Run `make build` after changes that affect the frontend to regenerate assets before building the binary.

Do not serve `index.html` through `http.FileServer` for SPA fallbacks: it redirects `index.html` paths and can cause deep-link redirect loops. Use the existing direct embedded-index handler.

Hashed assets receive `Cache-Control: public, max-age=7776000, immutable`; `index.html` must remain short-lived. Room routes must retain `X-Robots-Tag: noindex, nofollow, noarchive`.

## Docker release workflow

`docker/Dockerfile` is the canonical release build. It builds the frontend with pnpm, copies the assets into the Go build stage, and runs the resulting static binary as a non-root Alpine user.

- Do not embed credentials, Docker Hub namespaces, or environment-specific config in the image.
- Build using `make docker-build IMAGE=<namespace>/planning-poker TAG=<tag>`.
- Push only when explicitly requested, using `make docker-push` after `docker login`.
- Keep `.dockerignore` current when adding caches or generated output.

## Required checks

Run checks appropriate to changed areas before handoff:

```bash
cd backend && GOCACHE=/tmp/planningpoker-go-cache go test ./...
cd backend && GOCACHE=/tmp/planningpoker-go-cache go vet ./...
cd frontend && pnpm lint:fix && pnpm build
make build
```

Use `golangci-lint run` when a current golangci-lint built with Go 1.26+ is available. Add focused Go tests for changes to validation, roles, room lifecycle, or protocol behavior. For UI changes, verify both desktop and mobile layouts.

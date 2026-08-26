# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend-build
WORKDIR /workspace
RUN corepack enable
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./frontend/
WORKDIR /workspace/frontend
RUN pnpm install --frozen-lockfile
WORKDIR /workspace
COPY frontend ./frontend
RUN mkdir -p /workspace/backend/pkg/httpapi/web && cd frontend && pnpm build

FROM golang:1.26-alpine AS backend-build
WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
COPY --from=frontend-build /workspace/backend/pkg/httpapi/web ./pkg/httpapi/web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /planning-poker ./cmd/server

FROM alpine:3.22 AS runtime
RUN addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=backend-build /planning-poker ./planning-poker
COPY --from=backend-build /app/config/config.yaml ./config/config.yaml
USER app
EXPOSE 8080
ENV CONFIG_PATH=/app/config/config.yaml
ENTRYPOINT ["/app/planning-poker"]

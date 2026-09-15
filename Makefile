SHELL=/bin/bash
IMAGE ?= markus621/planning-poker-agile
TAG ?= latest

.PHONY: install install-backend install-frontend lint lint-backend lint-frontend test test-backend test-frontend build build-backend build-frontend run-backend run-frontend docker-build docker-push docker-run docker-up docker-down

install: install-backend install-frontend
install-backend:
	cd backend && go mod download
install-frontend:
	cd frontend && pnpm install --frozen-lockfile

lint: lint-backend lint-frontend
lint-backend:
	cd backend && golangci-lint run
lint-frontend:
	cd frontend && pnpm lint

test: test-backend test-frontend
test-backend:
	cd backend && GOCACHE=/tmp/planningpoker-go-cache go test ./...
test-frontend:
	cd frontend && pnpm test

build: build-backend
build-backend: build-frontend
	cd backend && GOCACHE=/tmp/planningpoker-go-cache go build ./cmd/server
build-frontend:
	cd frontend && pnpm build

run-backend: build-frontend
	cd backend && GOCACHE=/tmp/planningpoker-go-cache go run ./cmd/server
run-frontend:
	cd frontend && pnpm start

docker-build:
	docker buildx build \
    		-f ./docker/Dockerfile \
    		-t $(IMAGE):$(TAG) \
    		-t $(IMAGE):latest \
    		--platform linux/amd64,linux/arm64 \
    		--build-arg BUILDKIT_MULTI_PLATFORM=true \
    		--push .

docker-run:
	docker run --rm --publish 127.0.0.1:8080:8080 $(IMAGE):$(TAG)
docker-up:
	docker compose --file docker/docker-compose.yml up --build -d
docker-down:
	docker compose --file docker/docker-compose.yml down

license:
	goppy license

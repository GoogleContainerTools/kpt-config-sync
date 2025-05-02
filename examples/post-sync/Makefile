# Docker image configuration
REGION ?= us-central1
PROJECT_ID ?= your-project-id
IMAGE_NAME ?= sync-status-watch
GAR_REPO_NAME ?= sync-status-watch
IMAGE_TAG ?= latest
IMAGE_URI = ${REGION}-docker.pkg.dev/${PROJECT_ID}/${GAR_REPO_NAME}/${IMAGE_NAME}:${IMAGE_TAG}

# System architecture defaults
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Build output directory
OUTPUT_DIR = .output/go/bin/$(GOARCH)

# Supported platforms for multi-arch build
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: build push buildx-setup test

# Setup buildx builder for multi-arch builds
buildx-setup:
	@echo "Setting up Docker buildx..."
	docker buildx create --use --name multi-arch-builder || true

# Build multi-arch images
build: buildx-setup
	@echo "Building multi-arch Docker images..."
	docker buildx build --platform=$(PLATFORMS) \
		--build-arg GOOS=linux \
		-t $(IMAGE_URI) \
		--push \
		.

# Build for a single platform (useful for local development)
build-local:
	@echo "Building Docker image for local architecture..."
	docker build \
		--build-arg GOOS=$(GOOS) \
		--build-arg GOARCH=$(GOARCH) \
		-t $(IMAGE_URI) .

# Build the binary
build-binary:
	@echo "Building binary..."
	mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(OUTPUT_DIR)/sync-status-watch .

# Push is now part of the build target when using buildx
push: build

deploy: build
	@echo "Deploying to Kubernetes..."
	sed "s|SYNC_STATUS_WATCH_CONTROLLER_IMAGE|${IMAGE_URI}|g" sync-watch-manifest.yaml | kubectl apply -f -

# Run unit tests
test:
	@echo "Running unit tests..."
	go test -v ./...

# Clean up buildx builder and output directory
clean:
	docker buildx rm multi-arch-builder || true
	rm -rf .output

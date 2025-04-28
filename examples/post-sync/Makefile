# Docker image configuration
REGION ?= us-central1
PROJECT_ID ?= YOUR_PROJECT_ID
IMAGE_NAME ?= sync-status-watch
GAR_REPO_NAME ?= sync-status-watch
IMAGE_TAG ?= latest
IMAGE_URI = ${REGION}-docker.pkg.dev/${PROJECT_ID}/${GAR_REPO_NAME}/${IMAGE_NAME}:${IMAGE_TAG}

# Supported platforms for multi-arch build
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: build push buildx-setup

# Setup buildx builder for multi-arch builds
buildx-setup:
	@echo "Setting up Docker buildx..."
	docker buildx create --use --name multi-arch-builder || true

# Build multi-arch images
build: buildx-setup
	@echo "Building multi-arch Docker images..."
	docker buildx build --platform=$(PLATFORMS) \
		-t $(IMAGE_URI) \
		--push \
		.

# Build for a single platform (useful for local development)
build-local:
	@echo "Building Docker image for local architecture..."
	docker build -t $(IMAGE_URI) .

# Push is now part of the build target when using buildx
push: build

deploy: build
	@echo "Deploying to Kubernetes..."
	sed "s|SYNC_STATUS_WATCH_CONTROLLER_IMAGE|${IMAGE_URI}|g" sync-watch-manifest.yaml | kubectl apply -f -

# Clean up buildx builder
clean:
	docker buildx rm multi-arch-builder || true

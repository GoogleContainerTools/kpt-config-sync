# Docker image configuration
REGION ?= us-central1
PROJECT_ID ?= YOUR_PROJECT_ID
IMAGE_NAME ?= sync-status-watch
GAR_REPO_NAME ?= sync-status-watch
IMAGE_TAG ?= latest
IMAGE_URI = ${REGION}-docker.pkg.dev/${PROJECT_ID}/${GAR_REPO_NAME}/${IMAGE_NAME}:${IMAGE_TAG}

.PHONY: build push

build:
	@echo "Building Docker image..."
	docker build -t $(IMAGE_URI) .

push: build
	@echo "Pushing to Artifact Registry..."
	docker push $(IMAGE_URI)

deploy: push
	@echo "Deploying to Kubernetes..."
	sed "s|SYNC_STATUS_WATCH_CONTROLLER_IMAGE_REGISTRY|${IMAGE_URI}|g" sync-watch-manifest.yaml | kubectl apply -f -

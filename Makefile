
# Variables
REGISTRY ?= quay.io/open-cluster-management
IMAGE_TAG ?= latest

build-app-image:
	docker build -t ${REGISTRY}/sidecar:${IMAGE_TAG} . -f Dockerfile

push-app-image:
	docker push ${REGISTRY}/sidecar:${IMAGE_TAG}

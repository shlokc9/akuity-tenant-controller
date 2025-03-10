SHELL ?= /bin/bash

DOCKER_CMD := docker run \
	-it \
	--rm \
	-v $(HOME)/.akuity/gomodcache:/go/pkg/mod \
	-v $(dir $(realpath $(firstword $(MAKEFILE_LIST)))):/workspaces/tenant-controller \
	-w /workspaces/tenant-controller \
	tenant-controller:dev-tools

GET_IMAGE_DIGEST := docker inspect --format='{{.Id}}' tenant-controller:dev | cut -c 8-

.PHONY: build-dev-tools
build-dev-tools:
	docker build -f Dockerfile.dev -t tenant-controller:dev-tools .

.PHONY: lint
lint: build-dev-tools
	$(DOCKER_CMD) golangci-lint run --out-format=colored-line-number

.PHONY: test
test:  build-dev-tools
	$(DOCKER_CMD) go test \
		-v \
		-timeout=60s \
		-race \
		-coverprofile=coverage.txt \
		-covermode=atomic \
		./...

.PHONY: codegen
codegen: build-dev-tools
	$(DOCKER_CMD) sh -c '\
		controller-gen \
			rbac:roleName=manager-role \
			crd \
			webhook \
			paths=./api/v1alpha1/... \
			output:crd:artifacts:config=manifests/crds \
		&& controller-gen \
			object:headerFile=hack/codegen/boilerplate.go.txt \
			paths=./...\
	'

.PHONY: build
build:
	docker build --tag tenant-controller:dev .

.PHONY: deploy
deploy: build-dev-tools build
	docker tag tenant-controller:dev tenant-controller:$$($(GET_IMAGE_DIGEST))
	# TODO: Uncomment one of the following lines as needed. Note that Orbstack's
	# and Docker Desktop's Kubernetes clusters may not require you to do anything
	# special to load images into the cluster.
	# kind load docker-image tenant-controller=tenant-controller:$$($(GET_IMAGE_DIGEST))
	# k3d image import tenant-controller=tenant-controller:$$($(GET_IMAGE_DIGEST))
	# minikube image load tenant-controller=tenant-controller:$$($(GET_IMAGE_DIGEST))
	rm -rf deploy
	cp -r manifests deploy
	$(DOCKER_CMD) sh -c " \
		cd deploy \
		&& kustomize edit set image tenant-controller=tenant-controller:$$($(GET_IMAGE_DIGEST)) \
		&& kustomize build . > manifests.yaml \
	"
	kubectl apply -f deploy/manifests.yaml

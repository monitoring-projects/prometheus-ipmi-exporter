# Copyright 2021 The Prometheus Authors
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Override the default common all.
.PHONY: all
all: precheck style unused build test

PREFIX                  ?= $(shell pwd)
BIN_DIR                 ?= $(shell pwd)
DOCKER_IMAGE_NAME       ?= prometheuscommunity/ipmi-exporter

BRANCH      ?= $(shell git rev-parse --abbrev-ref HEAD)
BUILDDATE   ?= $(shell date --iso-8601=seconds)
BUILDUSER   ?= $(shell whoami)@$(shell hostname)
REVISION    ?= $(shell git rev-parse HEAD)
TAG_VERSION ?= $(shell git describe --tags --abbrev=0)

VERSION_LDFLAGS := \
  -X github.com/prometheus/common/version.Branch=$(BRANCH) \
  -X github.com/prometheus/common/version.BuildDate=$(BUILDDATE) \
  -X github.com/prometheus/common/version.BuildUser=$(BUILDUSER) \
  -X github.com/prometheus/common/version.Revision=$(REVISION) \
  -X github.com/prometheus/common/version.Version=$(TAG_VERSION)

# Include the common Makefile
include Makefile.common

# Additional targets
.PHONY: build-docker docker-release release

build:
	@echo ">> building code"
	CGO_ENABLED=0 go build -a -tags 'netgo static_build' -ldflags "$(VERSION_LDFLAGS)" -o ipmi_exporter .

docker:
	@echo ">> building docker image (standard)"
	docker build --build-arg BUILDPLATFORM=linux/amd64 --build-arg TARGETARCH=amd64 -t "$(DOCKER_IMAGE_NAME):$(TAG_VERSION)" .

docker-buildx:
	@echo ">> setting up buildx"
	docker buildx inspect --bootstrap
	docker buildx create --use --name multiarch --driver docker-container 2>/dev/null || true
	docker buildx use multiarch

docker-multiarch:
	@echo ">> building multi-arch docker image"
	$(MAKE) docker-buildx
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg BUILDPLATFORM=linux/amd64 -t "$(DOCKER_IMAGE_NAME):$(TAG_VERSION)" -t "$(DOCKER_IMAGE_NAME):latest" --load .

docker-multiarch-push:
	@echo ">> building and pushing multi-arch docker image"
	$(MAKE) docker-buildx
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg BUILDPLATFORM=linux/amd64 -t "$(DOCKER_IMAGE_NAME):$(TAG_VERSION)" -t "$(DOCKER_IMAGE_NAME):latest" --push .

docker-release:
	@echo ">> building and releasing docker image to ghcr"
	$(MAKE) docker-buildx
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg BUILDPLATFORM=linux/amd64 -t "ghcr.io/prometheus-community/ipmi-exporter:$(TAG_VERSION)" -t "ghcr.io/prometheus-community/ipmi-exporter:latest" --push .

release:
	@echo ">> releasing with goreleaser"
	goreleaser release --rm-dist

snapshot:
	@echo ">> creating snapshot with goreleaser"
	goreleaser release --snapshot --rm-dist

# Local testing commands
.PHONY: test-local test-docker test-docker-buildx test-release docker-local docker-test docker-test-debug docker-compose-up docker-compose-down docker-run-local docker-run-remote docker-run-local-sudo docker-run-custom docker-test-local docker-test-remote docker-test-local-sudo

test-local:
	@echo ">> testing goreleaser configuration locally"
	goreleaser release --snapshot --clean --skip=publish

test-docker:
	@echo ">> testing standard docker build locally"
	docker build --build-arg BUILDPLATFORM=linux/amd64 --build-arg TARGETARCH=amd64 -t ipmi-exporter:test .

test-docker-buildx:
	@echo ">> testing docker buildx multi-arch build locally"
	$(MAKE) docker-buildx
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg BUILDPLATFORM=linux/amd64 -t ipmi-exporter:test --load .

test-release:
	@echo ">> testing full release process locally (dry run)"
	goreleaser release --clean --skip=validate --skip=publish

# Local Docker development commands
docker-local:
	@echo ">> building local Docker image with multi-stage build"
	docker build -f Dockerfile.local -t ipmi-exporter:local .

docker-test:
	@echo ">> running IPMI exporter test mode in Docker"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-local.yml:/etc/ipmi-exporter/ipmi-local.yml:ro \
		ipmi-exporter:local --test

docker-test-debug:
	@echo ">> running IPMI exporter test mode in Docker with debug output"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-local.yml:/etc/ipmi-exporter/ipmi-local.yml:ro \
		ipmi-exporter:local --test --test.debug

# Configuration mode targets
docker-run-local:
	@echo ">> running IPMI exporter in local mode"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-local.yml:/etc/ipmi-exporter/ipmi-local.yml:ro \
		-p 9290:9290 ipmi-exporter:local --config.mode=local

docker-run-remote:
	@echo ">> running IPMI exporter in remote mode"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-remote.yml:/etc/ipmi-exporter/ipmi-remote.yml:ro \
		-p 9290:9290 ipmi-exporter:local --config.mode=remote

docker-run-local-sudo:
	@echo ">> running IPMI exporter in local-sudo mode"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-local-sudo.yml:/etc/ipmi-exporter/ipmi-local-sudo.yml:ro \
		-p 9290:9290 ipmi-exporter:local --config.mode=local-sudo

docker-run-custom:
	@echo ">> running IPMI exporter in custom mode"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-custom.yml:/etc/ipmi-exporter/ipmi-custom.yml:ro \
		-p 9290:9290 ipmi-exporter:local --config.mode=custom

docker-test-local:
	@echo ">> running IPMI exporter test mode in local configuration"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-local.yml:/etc/ipmi-exporter/ipmi-local.yml:ro \
		ipmi-exporter:local --config.mode=local --test

docker-test-remote:
	@echo ">> running IPMI exporter test mode in remote configuration"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-remote.yml:/etc/ipmi-exporter/ipmi-remote.yml:ro \
		ipmi-exporter:local --config.mode=remote --test

docker-test-local-sudo:
	@echo ">> running IPMI exporter test mode in local-sudo configuration"
	docker run --rm --privileged \
		-v $(PWD)/packaging/conf/ipmi-local-sudo.yml:/etc/ipmi-exporter/ipmi-local-sudo.yml:ro \
		ipmi-exporter:local --config.mode=local-sudo --test

docker-compose-up:
	@echo ">> starting local development environment"
	docker-compose -f docker-compose.local.yml up -d

docker-compose-down:
	@echo ">> stopping local development environment"
	docker-compose -f docker-compose.local.yml down

docker-compose-logs:
	@echo ">> showing logs from local development environment"
	docker-compose -f docker-compose.local.yml logs -f

# Act workflow testing (requires act CLI)
.PHONY: test-workflow docker-compose-logs

test-workflow:
	@echo ">> testing GitHub workflow locally with act"
	act -j test -W .github/workflows/test.yml

test-release-workflow:
	@echo ">> testing release workflow locally with act (dry run)"
	act -j goreleaser -W .github/workflows/release.yml --dry-run

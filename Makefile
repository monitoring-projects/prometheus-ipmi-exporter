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
.PHONY: test-local test-docker test-docker-buildx test-release

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

# Act workflow testing (requires act CLI)
.PHONY: test-workflow

test-workflow:
	@echo ">> testing GitHub workflow locally with act"
	act -j test -W .github/workflows/test.yml

test-release-workflow:
	@echo ">> testing release workflow locally with act (dry run)"
	act -j goreleaser -W .github/workflows/release.yml --dry-run

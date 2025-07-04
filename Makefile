#Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test

BINARY_NAME := ib-exporter
BINARY_PATH := ./bin/$(BINARY_NAME)

VERSION ?= $(shell git describe --tags --always --dirty | cut -d"v" -f2)

ARCH        ?=amd64
TARGET_OS   ?=linux

GO_BUILD_VARS       = GO111MODULE=on GOOS=$(TARGET_OS) GOARCH=$(ARCH) CGO_ENABLED=1
DOCKER_PLATFORM     = $(TARGET_OS)/$(ARCH)

IMG ?= ghcr.io/shiyak-infra/infiniband-exporter:${VERSION}

# Directories
CMD_DIR := ./cmd

default: build

build:
	$(GOBUILD) -o $(BINARY_PATH) $(CMD_DIR)

clean:
	$(GOCLEAN)
	rm -rf $(BINARY_PATH)

docker-build:
	docker build --platform ${DOCKER_PLATFORM} -t ${IMG} -f ./Dockerfile .

docker-push: docker-build
	docker push ${IMG}
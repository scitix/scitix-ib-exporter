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
	CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -ldflags '-extldflags "-static"' -o $(BINARY_PATH) $(CMD_DIR)

# 静态链接构建（推荐用于不同GLIBC版本的机器）
build-static:
	CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -ldflags '-extldflags "-static"' -o $(BINARY_PATH) $(CMD_DIR)

# 动态链接构建（默认）
build-dynamic:
	$(GOBUILD) -o $(BINARY_PATH) $(CMD_DIR)

# 为较老系统构建（禁用CGO）
build-portable:
	CGO_ENABLED=0 $(GOBUILD) -o $(BINARY_PATH) $(CMD_DIR)

clean:
	$(GOCLEAN)
	rm -rf $(BINARY_PATH)

docker-build:
	docker build --platform ${DOCKER_PLATFORM} -t ${IMG} -f ./Dockerfile .

docker-push: docker-build
	docker push ${IMG}
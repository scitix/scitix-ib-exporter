#Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test

BINARY_NAME := ib-exporter
BINARY_PATH := ./bin/$(BINARY_NAME)

# Directories
CMD_DIR := ./cmd

default: build

build:
	$(GOBUILD) -o $(BINARY_PATH) $(CMD_DIR)

clean:
	$(GOCLEAN)
	rm -rf $(BINARY_PATH)
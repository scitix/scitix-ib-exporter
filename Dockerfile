FROM golang:1.24.4 AS builder
COPY ./ /ib-exporter
WORKDIR /ib-exporter
RUN make build

FROM registry-cn-shanghai.siflow.cn/hpc/mlnx-ofed:24.10-2.1.8.0
ARG ARCH="amd64"
ARG OS="linux"
FROM alpine:latest
RUN apk add --no-cache \
    curl \
    ca-certificates
ARG ARCH="amd64"
ARG OS="linux"
COPY ./bin/ib-exporter /usr/bin/ib_exporter
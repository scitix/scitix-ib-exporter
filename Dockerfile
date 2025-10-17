FROM golang:1.24.4 AS builder
COPY ./ /ib-exporter
WORKDIR /ib-exporter
RUN make build

FROM registry-cn-shanghai.siflow.cn/hpc/mlnx-ofed:24.10-2.1.8.0
ARG ARCH="amd64"
ARG OS="linux"
# add harbor address
# ARG HARBOR="registry-cn-beijing.siflow.cn"
FROM ${HARBOR}/hpc/ofed:23.10-1.1.9.0-1
ARG ARCH="amd64"
ARG OS="linux"
COPY ./bin/ib-exporter /usr/local/bin/ib_exporter

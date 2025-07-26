FROM golang:1.24.4 AS builder
COPY ./ /ib-exporter
WORKDIR /ib-exporter
RUN make build

FROM registry-cn-shanghai.siflow.cn/hpc/mlnx-ofed:24.10-2.1.8.0
ARG ARCH="amd64"
ARG OS="linux"
COPY --from=builder /ib-exporter/bin/ib-exporter /usr/local/bin/ib_exporter
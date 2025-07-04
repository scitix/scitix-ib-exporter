FROM golang:1.24.4 AS builder
COPY ./ /ib-exporter
WORKDIR /ib-exporter
RUN make build

FROM ubuntu:22.04
COPY --from=builder /ib-exporter/bin/ib-exporter /usr/local/bin/ib_exporter
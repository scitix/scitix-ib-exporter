ARG ARCH="amd64"
ARG OS="linux"
FROM alpine:latest
RUN apk add --no-cache \
    curl pciutils\
    ca-certificates
ARG ARCH="amd64"
ARG OS="linux"
COPY ./bin/ib-exporter /usr/bin/ib_exporter
ARG ARCH="amd64"
ARG OS="linux"
# add harbor address
ARG HARBOR="registry-cn-beijing.siflow.cn"
FROM ${HARBOR}/hpc/ofed:23.10-1.1.9.0-1
ARG ARCH="amd64"
ARG OS="linux"
COPY ./bin/ib-exporter /usr/local/bin/ib_exporter

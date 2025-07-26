ARG ARCH="amd64"
ARG OS="linux"
# add harbor address
# ARG HARBOR="use harbor addr"
# FROM registry-cn-shanghai.siflow.cn/hpc/ofed:23.10-1.1.9.0-1

FROM registry-cn-shanghai.siflow.cn/hpc/mlnx-ofed:24.10-2.1.8.0
ARG ARCH="amd64"
ARG OS="linux"
COPY ./bin/ib-exporter /usr/local/bin/ib_exporter

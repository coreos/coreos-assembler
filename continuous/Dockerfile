FROM registry.ci.openshift.org/coreos/coreos-assembler:latest
USER root
ADD fcos-continuous.repo /etc/yum.repos.d
RUN yum -y update
USER builder

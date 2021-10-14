# "Build Root" definition for providing almost all dependencies
# for building CoreOS Assembler. This image is suitable for use
# in CI (Jenkins, Prow) and developer situations.
#
# Published as quay.io/coreos-assembler/cosa-buildroot

FROM registry.fedoraproject.org/fedora:34
WORKDIR /root/containerbuild

# Keep this section in sync with `Dockerfile.
# Only need a few of our scripts for the first few steps
COPY ./src/print-dependencies.sh ./src/deps*.txt ./src/vmdeps*.txt ./src/build-deps.txt /root/containerbuild/src/
COPY ./build.sh /root/containerbuild/
RUN ./build.sh configure_yum_repos
RUN ./build.sh install_rpms  # nocache 20211014

# Allow Prow to work
RUN mkdir -p /go && chown 0777 /go

ENTRYPOINT ["/usr/bin/dumb-init"]

# See ci/Dockerfile for the buildroot definition
FROM quay.io/coreos-assembler/cosa-buildroot:latest
WORKDIR /root/containerbuild

# We semi-support skipping the buildroot and just using e.g. `FROM fedora:34` as a base image,
# so keep this in sync with `ci/Dockerfile`.
COPY ./src/print-dependencies.sh ./src/deps*.txt ./src/vmdeps*.txt ./src/build-deps.txt /root/containerbuild/src/
COPY ./build.sh /root/containerbuild/
RUN ./build.sh configure_yum_repos
RUN ./build.sh install_rpms

COPY ./ /root/containerbuild/
RUN ./build.sh write_archive_info
RUN ./build.sh make_and_makeinstall
RUN ./build.sh configure_user

# clean up scripts (it will get cached in layers, but oh well)
WORKDIR /srv/
RUN chown builder: /srv
RUN rm -rf /root/containerbuild /go

# allow writing to /etc/passwd from arbitrary UID
# https://docs.openshift.com/container-platform/3.10/creating_images/guidelines.html
RUN chmod g=u /etc/passwd

# run as `builder` user
USER builder
ENTRYPOINT ["/usr/bin/dumb-init", "/usr/bin/coreos-assembler"]

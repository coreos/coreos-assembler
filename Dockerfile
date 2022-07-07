# When rebasing to new Fedora, also update openshift/release:
# https://github.com/openshift/release/tree/master/ci-operator/config/coreos/coreos-assembler/coreos-coreos-assembler-main.yaml
FROM registry.fedoraproject.org/fedora:36
WORKDIR /root/containerbuild

# Keep this Dockerfile idempotent for local development rebuild use cases.
USER root
RUN rm -rfv /usr/lib/coreos-assembler /usr/bin/coreos-assembler

COPY ./src/print-dependencies.sh ./src/deps*.txt ./src/vmdeps*.txt ./src/build-deps.txt /root/containerbuild/src/
COPY ./build.sh /root/containerbuild/
RUN ./build.sh configure_yum_repos
RUN ./build.sh install_rpms  # nocache 07/07/22
RUN ./build.sh install_ocp_tools

# This allows Prow jobs for other projects to use our cosa image as their
# buildroot image (so clonerefs can copy the repo into `/go`). For cosa itself,
# this same hack is inlined in the YAML (see openshift/release link above).
RUN mkdir -p /go && chmod 777 /go

COPY ./ /root/containerbuild/
RUN ./build.sh write_archive_info
RUN ./build.sh make_and_makeinstall
RUN ./build.sh configure_user

# clean up scripts (it will get cached in layers, but oh well)
WORKDIR /srv/
RUN chown builder: /srv
RUN rm -rf /root/containerbuild /go

# allow writing to /etc/passwd from arbitrary UID
# https://docs.openshift.com/container-platform/4.8/openshift_images/create-images.html
RUN chmod g=u /etc/passwd

# run as `builder` user
USER builder
ENTRYPOINT ["/usr/bin/dumb-init", "/usr/bin/coreos-assembler"]

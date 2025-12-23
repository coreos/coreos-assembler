# When rebasing to new Fedora, also update openshift/release:
# https://github.com/openshift/release/tree/master/ci-operator/config/coreos/coreos-assembler/coreos-coreos-assembler-main.yaml
FROM quay.io/fedora/fedora:43
WORKDIR /root/containerbuild
# This variable is enabled by Konflux to build the container image hermatically.
ARG NO_NETWORK=0
# Keep this Dockerfile idempotent for local development rebuild use cases.
USER root

COPY ./src/print-dependencies.sh ./src/deps*.txt ./src/vmdeps*.txt ./src/build-deps.txt /root/containerbuild/src/
COPY ./build.sh /root/containerbuild/
RUN rm -rfv /usr/lib/coreos-assembler /usr/bin/coreos-assembler \
    && ./build.sh configure_yum_repos \
    && ./build.sh install_rpms \
    && ./build.sh install_ocp_tools \
    && ./build.sh trust_redhat_gpg_keys

COPY ./ /root/containerbuild/
RUN ./build.sh write_archive_info \
    && ./build.sh make_and_makeinstall \
    && ./build.sh configure_user \
    && ./build.sh patch_osbuild \
    # clean up scripts (it will get cached in layers, but oh well)
    && chown builder: /srv \
    && rm -rf /root/containerbuild \
    # allow writing to /etc/passwd from arbitrary UID
    # https://docs.openshift.com/container-platform/4.8/openshift_images/create-images.html
    && chmod g=u /etc/passwd \
    # also allow adding certificates
    && chmod -R g=u /etc/pki/ca-trust \
    # This allows Prow jobs for other projects to use our cosa image as their
    # buildroot image (so clonerefs can copy the repo into `/go`). For cosa itself,
    # this same hack is inlined in the YAML (see openshift/release link above).
    && mkdir -p /go && chmod 777 /go

# run as `builder` user	
USER builder
ENTRYPOINT ["/usr/bin/dumb-init", "/usr/bin/coreos-assembler"]

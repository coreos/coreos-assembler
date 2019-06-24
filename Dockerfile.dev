# Instead of doing a slow, whole-cloth build for developer
# tests, you can quickly test changes via this file. This
# especially useful for changes to dependencies or for final
# verification of COSA code.
#
# This moves the build time from ~15 minutes to ~3-5 minutes.
# Example: buildah bud -f Dockerfile.dev -t localhost/cosa-test .

FROM quay.io/coreos-assembler/coreos-assembler:latest
WORKDIR /root/containerbuild

USER root
RUN rm -rfv /lib/coreos-assembler /usr/bin/coreos-assembler

COPY ./src/cmdlib.sh ./build.sh ./deps*.txt ./vmdeps.txt ./build-deps.txt /root/containerbuild/
RUN ./build.sh install_rpms

COPY ./ /root/containerbuild/
RUN ./build.sh write_archive_info
RUN ./build.sh make_and_makeinstall

RUN make clean

WORKDIR /srv/
RUN rm -rf /root/containerbuild

# run as `builder` user
USER builder
ENTRYPOINT ["/usr/bin/dumb-init", "/usr/bin/coreos-assembler"]

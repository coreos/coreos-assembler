FROM registry.fedoraproject.org/fedora:28
WORKDIR /root/src
COPY . /root/src
RUN ./build.sh
USER builder
ENTRYPOINT ["/usr/bin/coreos-assembler"]

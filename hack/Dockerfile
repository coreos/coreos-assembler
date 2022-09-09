# Use this for quicker iteration on coros-assembler; from the toplevel run e.g.:
# podman build -t localhost/cosa -f hack/Dockerfile .
FROM quay.io/coreos-assembler/coreos-assembler:latest
WORKDIR /src
COPY . .
USER root
RUN env GOFLAGS="-buildvcs=false" make && make install
USER builder

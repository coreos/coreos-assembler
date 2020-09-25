FROM registry.svc.ci.openshift.org/coreos/cosa-buildroot:latest as builder
COPY build.sh .
RUN ./build.sh

FROM quay.io/coreos-assembler/coreos-assembler:latest
COPY --from=builder /usr/lib/coreos-assembler/tests/kola/ /usr/lib/coreos-assembler/tests/kola/

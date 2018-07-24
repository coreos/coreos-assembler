FROM registry.fedoraproject.org/fedora:28
ADD build.sh /root
RUN mkdir /root/src
COPY Makefile coreos-* /root/src/
RUN ./root/build.sh && rm -f /root/build.sh # cache20180523
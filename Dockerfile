FROM registry.fedoraproject.org/fedora:28
ADD build.sh /root
ADD fahc.repo /etc/yum.repos.d/
RUN mkdir /root/src
COPY Makefile Cargo.toml /root/src/
COPY src /root/src/src
RUN ./root/build.sh && rm -f /root/build.sh # cache20180523

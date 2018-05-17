FROM registry.fedoraproject.org/fedora:28
RUN yum -y install rpm-ostree make cargo git && yum clean all
ADD build.sh /root
RUN mkdir /root/src
COPY Cargo.toml /root/src/
COPY src /root/src/src
RUN ./root/build.sh && rm -f /root/build.sh
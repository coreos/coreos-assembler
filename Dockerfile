FROM registry.fedoraproject.org/fedora:28
# rsync, python2, pygobject3-base are dependencies of ostree-releng-scripts
# Also add python3 so people can use that too.
RUN yum -y install rpm-ostree make cargo git \
    rsync pygobject3-base python3-gobject-base \
    && yum clean all
ADD build.sh /root
RUN mkdir /root/src
COPY Cargo.toml /root/src/
COPY src /root/src/src
RUN ./root/build.sh && rm -f /root/build.sh # cache20180523
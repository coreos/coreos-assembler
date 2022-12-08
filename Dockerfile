FROM registry.fedoraproject.org/fedora:34
WORKDIR /root/containerbuild

COPY ./src/print-dependencies.sh ./src/deps*.txt ./src/vmdeps*.txt ./src/build-deps.txt /root/containerbuild/src/
COPY ./build.sh /root/containerbuild/
RUN ./build.sh configure_yum_repos
RUN ./build.sh install_rpms
RUN ./build.sh install_ocp_tools

# Allow Prow to work
RUN mkdir -p /go && chown 0777 /go

COPY ./ /root/containerbuild/
RUN ./build.sh write_archive_info
RUN ./build.sh make_and_makeinstall
RUN ./build.sh configure_user

# clean up scripts (it will get cached in layers, but oh well)
WORKDIR /srv/
RUN chown builder: /srv
RUN rm -rf /root/containerbuild /go

# allow writing to /etc/passwd from arbitrary UID
# https://docs.openshift.com/container-platform/3.10/creating_images/guidelines.html
RUN chmod g=u /etc/passwd

# also allow adding certificates
RUN chmod -R g=u /etc/pki/ca-trust

# Add dnf repovar to be used in the RHCOS repos
RUN echo "4.9" > /etc/dnf/vars/ocprelease

# run as `builder` user
USER builder
ENTRYPOINT ["/usr/bin/dumb-init", "/usr/bin/coreos-assembler"]

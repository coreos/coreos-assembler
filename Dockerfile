FROM registry.fedoraproject.org/fedora:30
WORKDIR /root/containerbuild

# Only need a few of our scripts for the first few steps
COPY ./src/cmdlib.sh ./build.sh ./deps*.txt ./vmdeps*.txt ./build-deps.txt /root/containerbuild/
RUN ./build.sh configure_yum_repos
RUN ./build.sh install_rpms

# Ok copy in the rest of them for the next few steps
COPY ./ /root/containerbuild/
RUN ./build.sh write_archive_info
RUN ./build.sh install_anaconda
RUN ./build.sh make_and_makeinstall
RUN ./build.sh configure_user

# clean up scripts (it will get cached in layers, but oh well)
WORKDIR /srv/
RUN chown builder: /srv
RUN rm -rf /root/containerbuild

# allow writing to /etc/passwd from arbitrary UID
# https://docs.openshift.com/container-platform/3.10/creating_images/guidelines.html
RUN chmod g=u /etc/passwd

# run as `builder` user
USER builder
ENTRYPOINT ["/usr/bin/dumb-init", "/usr/bin/coreos-assembler"]

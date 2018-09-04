# Shared shell script library

fatal() {
    echo "error: $@" 1>&2; exit 1
}

preflight() {
    if [ $(stat -f --printf="%T" .) = "overlayfs" ]; then
        fatal "$(pwd) must be a volume"
    fi

    if ! stat /dev/kvm >/dev/null; then
        fatal "Unable to access /dev/kvm"
    fi

    if ! capsh --print | grep -q 'Current.*cap_sys_admin'; then
        fatal "This container must currently be run with --privileged"
    fi
}

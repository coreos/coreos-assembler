my_dir := $(abspath $(shell dirname $(lastword $(MAKEFILE_LIST))))
version = $(shell date +%Y-%m-%d).$(shell git rev-parse --short HEAD)~$(shell test -n "`git status -s`" && echo dirty || echo clean)
cosa_dir = $(shell test -d /usr/lib/coreos-assembler && echo /usr/lib/coreos-assembler)
ldflags=-X main.version=${version} -X main.cosaDir=${cosa_dir}

export PATH := $(my_dir)/bin:$(PATH)
PREFIX ?= /usr
DESTDIR ?=
ARCH:=$(shell uname -m)

pkgs := $(shell go list -mod=vendor ./...)
.PHONY: build
build: test minio
	cd cmd && go build  -i -ldflags "${ldflags}" -mod vendor -v -o ../bin/entry .

.PHONY: fmt
fmt:
	gofmt -d -e -l $(shell find . -iname "*.go"  -not -path "./vendor/*")
	golint ${pkgs}

.PHONY: minio
minio:
	@mkdir -p bin
	@test -x bin/minio || env GOBIN="${my_dir}/bin" go get github.com/minio/minio

.PHONY: test
test: fmt minio
	go test -mod=vendor -tags ci -i ${pkgs} && \
	go test -mod=vendor -tags ci -cover ${pkgs}

.PHONY: clean
clean:
	@go clean .
	@rm -rf bin

.PHONY: install
install: bin/entry bin/minio
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/entry
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/minio

my_uid = $(shell id -u)
.PHONY: devtest
devtest: build
	mkdir -p srv
	podman run --rm -i --tty \
		-a=stdin -a=stdout -a=stderr \
		--uidmap=$(my_uid):0:1 --uidmap=0:1:1000 --uidmap 1001:1001:64536 \
		--security-opt label=disable --privileged=true \
		--device /dev/fuse \
		--device /dev/kvm \
		--tmpfs /tmp \
		--volume=/var/tmp:/var/tmp \
		--volume=$(shell realpath .)/srv:/srv \
		--env="BUILD=`jq -cM "." ocp/build.json`" \
		--env="SOURCE_REPOSITORY=http://github.com/coreos/fedora-coreos-config" \
		--env="SOURCE_REF=testing-devel" \
		--env='COSA_CMDS=cosa fetch; cosa build;' \
		--volume=$(shell realpath .)/bin:/run/bin \
		--entrypoint='["/usr/bin/dumb-init", "/run/bin/entry"]' \
		quay.io/coreos-assembler/coreos-assembler:latest \
		builder

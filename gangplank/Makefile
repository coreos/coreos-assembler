my_dir := $(abspath $(shell dirname $(lastword $(MAKEFILE_LIST))))
state = $(shell test -n "`git status -s`" && echo dirty || echo clean)
version = $(shell cd .. && git log -n 1 --date=short --pretty=format:%cs.%h~${state} -- gangplank)
cosa_dir = $(shell test -d /usr/lib/coreos-assembler && echo /usr/lib/coreos-assembler)
ldflags=-X main.version=${version} -X main.cosaDir=${cosa_dir}

export PATH := $(my_dir)/bin:$(shell readlink -f ../tools/bin):$(PATH)

PREFIX ?= /usr
DESTDIR ?=
ARCH:=$(shell uname -m)

pkgs := $(shell go list -mod=vendor ./...)
.PHONY: build
build:
	cd "${my_dir}/cmd/gangway" && env CGO_ENABLED=0 go build  -ldflags "${ldflags}" -tags usergo,netgo,!podman,!minio -mod vendor -v -o ${my_dir}/bin/gangway
	cd "${my_dir}/cmd/gangplank" && go build -ldflags "${ldflags}" -tags podman,minio -mod vendor -v -o ${my_dir}/bin/gangplank .

.PHONY: docs
docs:
	for i in ocp cosa spec; do cd ${my_dir}/$$i; pwd; gomarkdoc \
		-u -o ../../docs/gangplank/api-$$i.md ./...; done \

.PHONY: fmt
fmt:
	gofmt -d -e -l $(shell find . -iname "*.go"  -not -path "./vendor/*")

.PHONY: staticanalysis
staticanalysis:
	golangci-lint run -v ./...

.PHONY: test
test: fmt
	go test -mod=vendor -tags ${test_tags} -v  -i ${pkgs} && \
	go test -mod=vendor -tags ${test_tags} -v -cover ${pkgs}

.PHONY: clean
clean:
	@go clean .
	@rm -rf bin

.PHONY: schema
schema:
	$(MAKE) -C ../tools schema

.PHONY: install
install: bin/gangplank
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/gangplank
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/gangway

.PHONY: go-deps
go-deps:
	go mod tidy
	go mod vendor
	go mod download

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
		--entrypoint='["/usr/bin/dumb-init", "/run/bin/gangplank"]' \
		quay.io/coreos-assembler/coreos-assembler:latest \
		builder

.PHONY: dev-image
dev-image: build
	$(MAKE) -C ../ocp build-dev

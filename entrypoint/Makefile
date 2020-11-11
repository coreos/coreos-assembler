my_dir := $(abspath $(shell dirname $(lastword $(MAKEFILE_LIST))))
version = $(shell date +%Y-%m-%d).$(shell git rev-parse --short HEAD)~$(shell test -n "`git status -s`" && echo dirty || echo clean)
cosa_dir = $(shell test -d /usr/lib/coreos-assembler && echo /usr/lib/coreos-assembler)
ldflags=-X main.version=${version} -X main.cosaDir=${cosa_dir}

export PATH := $(my_dir)/bin:$(shell readlink -f ../tools/bin):$(PATH)

PREFIX ?= /usr
DESTDIR ?=
ARCH:=$(shell uname -m)

pkgs := $(shell go list -mod=vendor ./...)
.PHONY: build
build: test
	cd cmd && go build  -i -ldflags "${ldflags}" -mod vendor -v -o ../bin/entry .

.PHONY: fmt
fmt:
	gofmt -d -e -l $(shell find . -iname "*.go"  -not -path "./vendor/*")
	golangci-lint run -v --timeout 5m ./...

.PHONY: test
test: fmt
	go test -mod=vendor -tags ${test_tags} -v  -i ${pkgs} && \
	go test -mod=vendor -tags ${test_tags} -v -cover ${pkgs}

.PHONY: clean
clean:
	@go clean .
	@rm -rf bin

.PHYON: schema
schema: schema_version = v1
schema:
	schematyper \
	                ../src/schema/$(schema_version).json \
	                -o cosa/v1.go \
	                --package=cosa \
	                --root-type=Build \
	                --ptr-for-omit

.PHONY: install
install: bin/entry
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/entry

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
		--entrypoint='["/usr/bin/dumb-init", "/run/bin/entry"]' \
		quay.io/coreos-assembler/coreos-assembler:latest \
		builder

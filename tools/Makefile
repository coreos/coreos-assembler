PREFIX ?= /usr
DESTDIR ?=

export GO11MODULE=on
export GOOS=linux
export CGO_ENABLED=0
export GOPATH=$(shell readlink -f .)
GOARCH := $(shell go env GOARCH)
build:
	mkdir -p bin
	test -e bin/golangci-lint || \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin
	test -f bin/schematyper || \
		go install github.com/idubinskiy/schematyper@latest
	test -e bin/gomarkdoc || \
		go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest
	# Freeze on 2022-05-26 release of minio for now.
	# https://github.com/coreos/coreos-assembler/pull/2895#issuecomment-1145558105
	test -e bin/minio || \
		curl -sSfL --output minio --output-dir $(GOPATH)/bin https://dl.min.io/server/minio/release/linux-$(GOARCH)/archive/minio.RELEASE.2022-05-26T05-48-41Z && chmod +x $(GOPATH)/bin/minio
	rm -rf pkg

clean:
	rm -rfv bin
	rm -rfv pkg

install: bin/golangci-lint bin/gomarkdoc bin/minio bin/schematyper
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/{golangci-lint,gomarkdoc,minio,schematyper}

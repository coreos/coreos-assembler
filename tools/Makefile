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
	test -e bin/minio || \
		curl -sSfLO --output-dir $(GOPATH)/bin https://dl.min.io/server/minio/release/linux-$(GOARCH)/minio && chmod +x $(GOPATH)/bin/minio
	rm -rf pkg

clean:
	rm -rfv bin
	rm -rfv pkg

install: bin/golangci-lint bin/gomarkdoc bin/minio bin/schematyper
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/{golangci-lint,gomarkdoc,minio,schematyper}

PREFIX ?= /usr
DESTDIR ?=

export GO11MODULE=on
export GOOS=linux
export CGO_ENABLED=0
export GOPATH=$(shell readlink -f .)
GOARCH := $(shell go env GOARCH)
build:
	mkdir -p bin
	test -f bin/schematyper || \
		go install github.com/idubinskiy/schematyper@latest
	test -e bin/gomarkdoc || \
		go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest
	rm -rf pkg

clean:
	rm -rfv bin
	rm -rfv pkg

install: bin/gomarkdoc bin/schematyper
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/{gomarkdoc,schematyper}

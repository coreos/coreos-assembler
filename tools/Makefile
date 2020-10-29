PREFIX ?= /usr
DESTDIR ?=

build:
	mkdir -p bin
	test -e bin/minio || \
	go build -o bin/minio ./vendor/github.com/minio/minio/cmd
	test -e bin/golangci-lint || \
	go build -o bin/golangci-lint ./vendor/github.com/golangci/golangci-lint/cmd/golangci-lint
	test -e bin/gosec || \
	go build -o bin/gosec ./vendor/github.com/securego/gosec/cmd/gosec

clean:
	rm -rfv bin

install: bin/minio bin/golangci-lint bin/gosec
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/{minio,golangci-lint,gosec}

go-deps:
	go mod tidy
	go mod download
	go mod vendor



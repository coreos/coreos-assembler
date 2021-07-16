PREFIX ?= /usr
DESTDIR ?=
toolsLong=$(shell cd .. && $ git log -n 1 --date=short --pretty=format:%H -- tools)
toolsShort=$(shell cd .. && $ git log -n 1 --date=short --pretty=format:%h -- tools)
toolsVersion=$(shell cd .. && $ git log -n 1 --date=short --pretty=format:%cs~%h -- tools)
minioFlags := -X github.com/minio/minio/cmd.Version=$(toolsVersion) -X github.com/minio/minio/cmd.ReleaseTag=COSA-$(toolsVersion) -X github.com/minio/minio/cmd.CommitID=cosa-$(toolsLong) -X github.com/minio/minio/cmd.ShortCommitID=cosa-$(toolsShort)

export GO11MODULE=on
export GOOS=linux
export CGO_ENABLED=0
build:
	mkdir -p bin
	test -e bin/golangci-lint || \
	go build -mod=vendor -o bin/golangci-lint github.com/golangci/golangci-lint/cmd/golangci-lint
	test -e bin/gosec || \
	go build -mod=vendor -o bin/gosec github.com/securego/gosec/cmd/gosec
	test -f bin/schematyper || \
	go build -mod=vendor -o bin/schematyper github.com/idubinskiy/schematyper
	test -e bin/gomarkdoc || \
	go build -mod=vendor -o bin/gomarkdoc github.com/princjef/gomarkdoc/cmd/gomarkdoc
	test -e bin/minio || \
	go build -mod=vendor -tags kqueue -trimpath --ldflags "$(minioFlags)" -o bin/minio github.com/minio/minio

clean:
	rm -rfv bin

install: bin/minio bin/golangci-lint bin/gosec bin/schematyper bin/gomarkdoc
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/{minio,golangci-lint,gosec}

go-deps:
	go mod tidy
	go mod download
	go mod vendor

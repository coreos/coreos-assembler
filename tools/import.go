package main

import (
	_ "github.com/gedex/inflector"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/idubinskiy/schematyper"
	_ "github.com/minio/minio"
	_ "github.com/princjef/gomarkdoc/cmd/gomarkdoc"
	_ "github.com/securego/gosec/cmd/gosec"
)

func main() {
	panic("noop")
}

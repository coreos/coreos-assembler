// +build tools

// tools is a dummy package that will be ignored for builds, but included for dependencies.
package tools

import (
	// Code generators built at runtime.
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/idubinskiy/schematyper"
	_ "github.com/minio/minio"
	_ "github.com/securego/gosec/cmd/gosec"
)

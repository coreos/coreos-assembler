// Package bintool manages local binary tool dependencies in your project.
//
// Tools are first defined with bintool.New(), which is provided with the tool's
// executable name, desired version, and template URL for downloading it if
// needed.
//
// For example, this call will configure version 1.23.6 of golangci-lint for
// use:
//
//	linter, err = bintool.New(
//		"golangci-lint{{.BinExt}}",
//		"1.23.6",
//		"https://github.com/golangci/golangci-lint/releases/download/v{{.Version}}/golangci-lint-{{.Version}}-{{.GOOS}}-{{.GOARCH}}{{.ArchiveExt}}",
//	)
//
// Since errors are only reported for templating issues, it's common to wrap the
// call with bintool.Must(), which will panic if an error is returned, rather
// than making you handle it:
//
//	linter := bintool.Must(bintool.New(
//		"golangci-lint{{.BinExt}}",
//		"1.23.6",
//		"https://github.com/golangci/golangci-lint/releases/download/v{{.Version}}/golangci-lint-{{.Version}}-{{.GOOS}}-{{.GOARCH}}{{.ArchiveExt}}",
//	))
//
// Templates
//
// Both the executable name and the download URL are configurable using
// templates so that they can easily be used cross-platform and with alternative
// versions using one set of code.
//
// Wherever a template can be used, the following variables are available:
//
//	- GOOS         The value of runtime.GOOS for your installation of go
//
//	- GOARCH       The value of runtime.GOARCH for your installation of go
//
//	- Version      The version of the tool specified on initialization
//
//	- Cmd          The evaluated name of the command template provided. Not
//	               available in the command template itself.
//
//	- FullCmd      The path to the command where it will be installed on your
//	               system.  By default, this is the result of evaluating
//	               ./bin/{{.Cmd}}
//
//	- ArchiveExt   The extension to use when defining an archive path. Defaults
//	               to .zip for Windows and .tar.gz for all other operating
//	               systems. Can be customized using bintool.WithArchiveExt().
//
//	- BinExt       The extension to use when defining a binary executable path.
//	               Defaults to .exe for Windows and is empty for all other
//	               operating systems. Can be customized using
//	               bintool.WithBinExt().
//
// Capabilities
//
// Once you've defined your tool, you can check for its existence with the
// correct version using IsInstalled(), install it using Install(), install it
// only if it isn't already installed using Ensure(), or run a command using the
// resulting binary with Command("rest of args"). See the individual method docs
// for more details.
//
// Configuration Options
//
// There are a few options to configure the tool at creation time. All are
// presented with a top-level function named "With<option>". They are provided
// like this:
//
//	linter := bintool.Must(bintool.New(
//		"golangci-lint{{.BinExt}}",
//		"1.23.6",
//		"https://github.com/golangci/golangci-lint/releases/download/v{{.Version}}/golangci-lint-{{.Version}}-{{.GOOS}}-{{.GOARCH}}{{.ArchiveExt}}",
//		bintool.WithFolder("./cmds"), // Put commands in a ./cmds folder
//	))
//
// To provide isolation between projects and ensure that your project has
// access to the exact version of the tooling you desire, the installed
// execuables are placed in a ./bin folder within your project. This location
// can be customized using bintool.WithFolder(). Paths are normalized for
// Windows, so they should be specified as unix-style paths.
//
// When determining whether the version of the command is correct, a command
// must be run to check the version against the expected one. This varies from
// tool to tool, but is assumed to be `{{.FullCmd}} --version` by default. If
// your tool uses a different version command, you can customize it using
// bintool.WithVersionCmd(). The provided command is a template accepting all of
// the parameters defined above.
//
// If your archive and/or binary files use different extensions than the default
// ones provided, you can customize them for your templates using
// bintool.WithArchiveExt() and bintool.WithBinExt(), respectively.
package bintool

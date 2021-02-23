// Package shellcmd provides utilites to define and execute shell commands.
//
// The core construct for executing commands is the shellcmd.Command type, which
// represents a command string as it would be typed into a terminal. Command
// strings follow the same quoting and escaping rules as a typical POSIX shell,
// but do not perform shell expansions.
//
// Once a command has been created, it can be run using the Run() method. This
// will print the command that is being run and will pipe its output to the
// terminal.
//
//	err := shellcmd.Command(`go test ./...`).Run()
//
// If you need to run multiple commands in sequence, you can do so with the
// shellcmd.RunAll() function. This will handle capturing errors in previous
// commands and skipping execution of later commands if they fail.
//
//	err := shellcmd.RunAll(
//		"go test -coverprofile=coverage.out ./...",
//		"go tool cover -html=coverage.out",
//	)
//
// Commands can also run in a mode that captures their output rather than piping
// it to the console. This is available via the Output() method.
//
//	out, err := shellcmd.Command(`go test ./...`).Output()
package shellcmd

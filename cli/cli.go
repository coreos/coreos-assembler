// Copyright 2014 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cli

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
)

var (
	cliName        string
	cliDescription string
	out            *tabwriter.Writer
	commands       []*Command // Commands must add themselves via Register()
	help           bool
	logDebug       bool
	logVerbose     bool
	logLevel       capnslog.LogLevel = capnslog.NOTICE
	plog           *capnslog.PackageLogger
)

func init() {
	flag.BoolVar(&help, "help", false, "Print usage information and exit. (-h)")
	flag.BoolVar(&help, "h", false, "")
	flag.BoolVar(&logDebug, "d", false, "Alias for --log-level=DEBUG")
	flag.BoolVar(&logVerbose, "v", false, "Alias for --log-level=INFO")
	flag.Var(&logLevel, "log-level", "Set global log level.")

	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "cli")
	out = new(tabwriter.Writer)
	out.Init(os.Stdout, 0, 8, 1, '\t', 0)
}

type Command struct {
	Name        string       // Name of the Command and the string to use to invoke it
	Summary     string       // One-sentence summary of what the Command does
	Usage       string       // Usage options/arguments
	Description string       // Detailed description of command
	Flags       flag.FlagSet // Set of flags associated with this command

	Run func(args []string) int // Run a command with the given arguments, return exit status

}

func Register(cmd *Command) {
	commands = append(commands, cmd)
}

func Executable() string {
	return cliName
}

func Description() string {
	return cliDescription
}

func Run(name, desc string) {
	cliName = name
	cliDescription = desc

	cmd, args := parseCommand()
	startLogging()

	os.Exit(cmd.Run(args))
}

func startLogging() {
	switch {
	case logDebug:
		logLevel = capnslog.DEBUG
	case logVerbose:
		logLevel = capnslog.INFO
	}

	capnslog.SetFormatter(capnslog.NewStringFormatter(os.Stderr))
	capnslog.SetGlobalLogLevel(logLevel)
	plog.Infof("Started %s logging on stderr", logLevel)
}

func parseCommand() (*Command, []string) {
	var cmd *Command

	// Parse global arguments that precede the command.
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		cmd = findCommand("help")
		help = false
	} else {
		cmd = findCommand(args[0])

		// Add command specific flags and resume parsing.
		updateCommandLine(&cmd.Flags)
		flag.CommandLine.Parse(args[1:])
		args = flag.Args()
	}

	if help {
		args = []string{cmd.Name}
		cmd = findCommand("help")
	}

	return cmd, args
}

func findCommand(name string) *Command {
	var cmd *Command

	for _, c := range commands {
		if c.Name == name {
			cmd = c
			break
		}
	}

	if cmd == nil {
		fmt.Fprintf(os.Stderr, "%v: unknown subcommand: %q\n", cliName, name)
		fmt.Fprintf(os.Stderr, "Run '%v help' for usage.\n", cliName)
		os.Exit(2)
	}

	return cmd
}

// Add subcommand specific flags to the globally known flags
func updateCommandLine(flagset *flag.FlagSet) {
	flagset.VisitAll(func(f *flag.Flag) {
		flag.Var(f.Value, f.Name, f.Usage)
	})
}

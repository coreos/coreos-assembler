// Copyright 2015 CoreOS, Inc.
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

package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/register"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

	root = &cobra.Command{
		Use:   "kola [command]",
		Short: "The CoreOS Superdeep Borehole",
		// http://en.wikipedia.org/wiki/Kola_Superdeep_Borehole
	}

	cmdRun = &cobra.Command{
		Use:    "run [glob pattern]",
		Short:  "Run run kola tests by category",
		Long:   "run all kola tests (default) or related groups",
		Run:    runRun,
		PreRun: preRun,
	}

	cmdList = &cobra.Command{
		Use:   "list",
		Short: "List kola test names",
		Run:   runList,
	}
)

func init() {
	root.AddCommand(cmdRun)
	root.AddCommand(cmdList)
}

func main() {
	cli.Execute(root)
}

func preRun(cmd *cobra.Command, args []string) {
	err := syncOptions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(3)
	}
}

func runRun(cmd *cobra.Command, args []string) {
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "Extra arguements specified. Usage: 'kola run [glob pattern]'\n")
		os.Exit(2)
	}
	var pattern string
	if len(args) == 1 {
		pattern = args[0]
	} else {
		pattern = "*" // run all tests by default
	}

	err := kola.RunTests(pattern, kolaPlatform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runList(cmd *cobra.Command, args []string) {
	var w = tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0)
	var testlist list

	for name, test := range register.Tests {
		testlist = append(testlist, item{name, test.Platforms})
	}

	sort.Sort(testlist)

	fmt.Fprintln(w, "Test Name\tPlatforms Available")
	fmt.Fprintln(w, "\t")
	for _, item := range testlist {
		fmt.Fprintf(w, "%v\n", item)
	}
	w.Flush()
}

type item struct {
	Name      string
	Platforms []string
}

func (i item) String() string {
	if len(i.Platforms) == 0 {
		i.Platforms = []string{"all"}
	}
	return fmt.Sprintf("%v\t%v", i.Name, i.Platforms)
}

type list []item

func (s list) Len() int           { return len(s) }
func (s list) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s list) Less(i, j int) bool { return s[i].Name < s[j].Name }

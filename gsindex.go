/*
   Copyright 2014 CoreOS, Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package main

import (
	"fmt"
	"os"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/index"
)

func main() {
	client, err := auth.GoogleClient(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	if err := index.Update(client, os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "Updating indexes failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Update successful!\n")
}

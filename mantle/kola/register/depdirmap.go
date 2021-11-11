// Copyright 2021 CoreOS, Inc.
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

package register

import (
	"fmt"
	"strings"
)

type DepDirMap map[string]string

// In the case of non-exclusive tests, some tests may have the same dependency dir
// as other tests. Since the tests will be run in one VM, the CopyDirToMachine func
// will only be able to sync to contents of dependency dir for one of the tests.

// We will use DepDirMap to keep track of which dependency dir maps to which destination
// folder. To do this we need both the dependency dir and name as keys since DependencyDir
// itself is not unique for all tests.

func (mp *DepDirMap) Add(name string, depDir string, destination string) {
	if *mp == nil {
		*mp = make(DepDirMap)
	} else if _, ok := (*mp)[name]; ok {
		panic(fmt.Errorf("kola: duplicate test in DepDirMap %q", name))
	}
	key := fmt.Sprintf("%s++%s", name, depDir)
	(*mp)[key] = destination
}

func (mp *DepDirMap) Get(name string, depDir string) string {
	if *mp == nil {
		panic(fmt.Errorf("kola: DepDirMap is empty"))
	}
	key := fmt.Sprintf("%s++%s", name, depDir)
	return (*mp)[key]
}

func (mp *DepDirMap) DirFromKey(key string) string {
	if _, ok := (*mp)[key]; !ok {
		panic(fmt.Errorf("kola: %q is not in DepDirMap", key))
	}
	arr := strings.SplitAfterN(key, "++", 2)
	return arr[1]
}

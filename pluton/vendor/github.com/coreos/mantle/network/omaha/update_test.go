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

package omaha

import (
	"encoding/xml"
	"testing"
)

const SampleUpdate = `<?xml version="1.0" encoding="UTF-8"?>
<update appid="{87efface-864d-49a5-9bb3-4b050a7c227a}" version="9999.0.0">
 <urls>
  <url codebase="packages/9999.0.0"></url>
 </urls>
 <manifest version="9999.0.0">
  <packages>
   <package name="update.gz" hash="+LXvjiaPkeYDLHoNKlf9qbJwvnk=" size="67546213" required="true"></package>
  </packages>
</update>
`

func TestUpdateURLs(t *testing.T) {
	u := Update{}
	xml.Unmarshal([]byte(SampleUpdate), &u)

	urls := u.URLs([]string{"http://localhost/updates/"})
	if urls[0].CodeBase != "http://localhost/updates/packages/9999.0.0" {
		t.Error("Unexpected URL", urls[0].CodeBase)
	}
}

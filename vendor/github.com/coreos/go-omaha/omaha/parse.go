// Copyright 2017 CoreOS, Inc.
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
	"fmt"
	"io"
	"mime"
	"strings"
)

// checkContentType verifies the HTTP Content-Type header properly
// declares the document is XML and UTF-8. Blank is assumed OK.
func checkContentType(contentType string) error {
	if contentType == "" {
		return nil
	}

	mType, mParams, err := mime.ParseMediaType(contentType)
	if err != nil {
		return err
	}

	if mType != "text/xml" && mType != "application/xml" {
		return fmt.Errorf("unsupported content type %q", mType)
	}

	charset, _ := mParams["charset"]
	if charset != "" && strings.ToLower(charset) != "utf-8" {
		return fmt.Errorf("unsupported content charset %q", charset)
	}

	return nil
}

// parseReqOrResp parses Request and Response objects.
func parseReqOrResp(r io.Reader, v interface{}) error {
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(v); err != nil {
		return err
	}

	var protocol string
	switch v := v.(type) {
	case *Request:
		protocol = v.Protocol
	case *Response:
		protocol = v.Protocol
	default:
		panic(fmt.Errorf("unexpected type %T", v))
	}

	if protocol != "3.0" {
		return fmt.Errorf("unsupported omaha protocol: %q", protocol)
	}

	return nil
}

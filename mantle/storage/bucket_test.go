// Copyright 2016 CoreOS, Inc.
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

package storage

import (
	"fmt"
	"net/http"
	"testing"

	"google.golang.org/api/storage/v1"
)

type fakeTransport struct{}

func (f fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("FAKE! %s %s", req.Method, req.URL)
}

func FakeBucket(bucketURL string) (*Bucket, error) {
	return NewBucket(&http.Client{Transport: fakeTransport{}}, bucketURL)
}

func (b *Bucket) AddObject(obj *storage.Object) {
	b.addObject(obj)
}

func TestBucketURL(t *testing.T) {
	if _, err := FakeBucket("http://bucket/"); err != UnknownScheme {
		t.Errorf("Unexpected error: %v", err)
	}

	if _, err := FakeBucket("gs:///"); err != UnknownBucket {
		t.Errorf("Unexpected error: %v", err)
	}

	for _, test := range []struct {
		url    string
		name   string
		prefix string
	}{
		{"gs://bucket", "bucket", ""},
		{"gs://bucket/", "bucket", ""},
		{"gs://bucket/prefix", "bucket", "prefix/"},
		{"gs://bucket/prefix/", "bucket", "prefix/"},
		{"gs://bucket/prefix/foo", "bucket", "prefix/foo/"},
		{"gs://bucket/prefix/foo/", "bucket", "prefix/foo/"},
	} {

		bkt, err := FakeBucket(test.url)
		if err != nil {
			t.Errorf("Unexpected error for url %q: %v", test.url, err)
			continue
		}

		if bkt.Name() != test.name {
			t.Errorf("Unexpected name for url %q: %q", test.url, bkt.Name())
		}
		if bkt.Prefix() != test.prefix {
			t.Errorf("Unexpected name for url %q: %q", test.url, bkt.Prefix())
		}
	}

}

func ExampleNextPrefix() {
	fmt.Println(NextPrefix("foo/bar/baz"))
	fmt.Println(NextPrefix("foo/bar/"))
	fmt.Println(NextPrefix("foo/bar"))
	fmt.Println(NextPrefix("foo/"))
	fmt.Println(NextPrefix("foo"))
	fmt.Println(NextPrefix(""))
	// Output:
	// foo/bar/
	// foo/
	// foo/
	//
	//
}

package cosa

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	fcosJSON   = "../../../fixtures/fcos.json"
	rhcosJSON  = "../../../fixtures/rhcos.json"
	cosaSchema = "../../../src/schema/v1.json"
)

var testMeta = []string{fcosJSON, rhcosJSON}

// Test schema validation on reading a file
func TestSchema(t *testing.T) {
	for _, df := range testMeta {
		b, err := ParseBuild(df)
		if err != nil {
			t.Errorf("failed to read %s: %v", df, err)
		}
		if b == nil {
			t.Errorf("failed to render build")
		}

		// Render it invalid
		if errs := b.Validate(); len(errs) > 0 {
			t.Errorf("validation should have failed")
		}

	}
}

// Test that we can write a file
func TestWriteMeta(t *testing.T) {
	tmpd, err := ioutil.TempDir("", "test-writemeta-*****")
	if err != nil {
		t.Errorf("failed to create tmpdir: %v", err)
	}
	defer os.RemoveAll(tmpd)

	for _, df := range testMeta {
		b, err := ParseBuild(df)
		if err != nil {
			t.Errorf("failed to read %s for write test", df)
		}

		tmpf := fmt.Sprintf("%s/valdiate.json", tmpd)
		if err = b.WriteMeta(tmpf, true); err != nil {
			t.Errorf("failed to write meta.json after validating: %v", err)
		}

		// No validation
		tmpf = fmt.Sprintf("%s/no_valdiate.json", tmpd)
		if err = b.WriteMeta(tmpf, false); err != nil {
			t.Errorf("failed to write meta.json without validation")
		}

	}
}

// Test that the schema can be set.
func TestSchemaSet(t *testing.T) {
	f, err := os.Open(cosaSchema)
	if err != nil {
		t.Errorf("failed to open %s: %v", cosaSchema, err)
	}

	if err := SetSchemaFromFile(f); err != nil {
		t.Errorf("failed to set schema from file: %v", err)
	}
}

// Test fetching from a URL
func TestBuildURL(t *testing.T) {
	for _, df := range testMeta {
		in, err := os.Open(df)
		if err != nil {
			t.Errorf("failed to open test data: %s: %v", df, err)
		}

		handler := func(w http.ResponseWriter, r *http.Request) {
			if _, err = io.CopyBuffer(w, in, nil); err != nil {
				t.Errorf("failed to serve test data from %s: %v", df, err)
			}
		}

		req := httptest.NewRequest("GET", "http://example.com/foo", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		resp := w.Result()
		_, err = buildParser(resp.Body)
		defer resp.Body.Close()
		if err != nil {
			t.Errorf("error reading url: %v", err)
		}
	}
}

func TestArtifact(t *testing.T) {
	for _, df := range testMeta {
		b, err := ParseBuild(df)
		if err != nil {
			t.Fatalf("failed to read %s: %v", df, err)
		}
		if b == nil {
			t.Fatal("failed to render build")
		}

		if b.BuildArtifacts.Aws == nil {
			continue
		}

		if _, err = b.GetArtifact("aws"); err != nil {
			t.Fatalf("Failed to get artifact: %v", err)
		}
	}
}

func TestMergeMeta(t *testing.T) {
	b, err := ParseBuild(fcosJSON)
	if err != nil {
		t.Fatalf("failed to read %s: %v", fcosJSON, err)
	}
	if b == nil {
		t.Fatal("failed to render build")
	}

	// create a copy of the build and then remove the artifacts
	data, _ := json.Marshal(b)
	var c Build
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal("failed to unmarshal meta")
	}
	// remove artifacts from c
	c.BuildArtifacts = nil
	c.BuildID = "foo"

	// remove AMIs from b and set delayed merge on
	b.Amis = nil
	b.CosaDelayedMetaMerge = true

	// Create a fake build structure
	tmpd, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("unable to create a tmpdir")
	}
	defer os.RemoveAll(tmpd) //nolint

	// Create a fake build dir
	fakeBuildID := "999.1"
	bjson, _ := json.Marshal(buildsJSON{
		SchemaVersion: "0.1.0",
		Builds: []build{
			{
				ID:     fakeBuildID,
				Arches: []string{BuilderArch()},
			},
		},
		TimeStamp: "meh",
	})

	fakeBuildDir := filepath.Join(tmpd, "builds", fakeBuildID, BuilderArch())
	if err := os.MkdirAll(fakeBuildDir, 0777); err != nil {
		t.Fatalf("failed to create test meta structure")
	}
	if err := ioutil.WriteFile(filepath.Join(tmpd, "builds", "builds.json"), bjson, 0644); err != nil {
		t.Fatalf("error creating builds.json")
	}
	if err := b.WriteMeta(filepath.Join(fakeBuildDir, "meta.json"), false); err != nil {
		t.Fatalf("failed to write tmp meta.json: %v", err)
	}
	if err := c.WriteMeta(filepath.Join(fakeBuildDir, "meta.test.json"), false); err != nil {
		t.Fatalf("failed to write tmp meta.test.json: %v", err)
	}

	// Now merge
	data, _ = json.Marshal(b)
	dR := bytes.NewReader(data)

	if err := c.mergeMeta(dR); err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	// Build Artifacts should be added into c
	if !reflect.DeepEqual(c.BuildArtifacts, b.BuildArtifacts) {
		t.Fatal("build artifacts are not equal")
	}
	// c.BuildID is set to 'foo', check its updated
	if c.BuildID != b.BuildID {
		t.Fatalf("buildID should have been updated:\n want: %s\n  got: %s\n", c.BuildID, b.BuildID)
	}

	// m represents the merger of b and c
	// where b is the starting meta.json
	// m.BuildID should be c.Build
	m, _, err := ReadBuild(filepath.Join(tmpd, "builds"), "", BuilderArch())
	if err != nil {
		t.Fatal("failed to find build")
	}
	if m == nil {
		t.Fatal("merge should not be nil")
	}

	if !reflect.DeepEqual(m.BuildArtifacts, b.BuildArtifacts) {
		t.Errorf("merge should not remove artifacts")
	}
	if !reflect.DeepEqual(m.Amis, c.Amis) {
		t.Errorf("merge should have AMIs")
	}
}

func TestMetaRegEx(t *testing.T) {
	testCases := []struct {
		data  string
		match bool
	}{
		{
			data:  "meta.json",
			match: true,
		},
		{
			data:  "meta.test.json",
			match: true,
		},
		{
			data:  "commitmmeta.json",
			match: false,
		},
		{
			data:  "meta.json.bk",
			match: false,
		},
		{
			data:  "metafoo.json",
			match: false,
		},
	}

	for idx, c := range testCases {
		t.Run(fmt.Sprintf("regex test %d %q", idx, string(c.data)), func(t *testing.T) {
			matched := IsMetaJSON(c.data)
			if matched != c.match {
				t.Errorf("%s:\n want: %v\n  got: %v\n", string(c.data), c.match, matched)
			}
		})
	}
}

package cosa

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

var (
	testMeta = []string{
		"../../fixtures/fcos.json",
		"../../fixtures/rhcos.json",
	}

	cosaSchema = "../../src/schema/v1.json"
)

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

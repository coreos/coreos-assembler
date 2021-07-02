package cosa

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

var testData = ` 
{
    "schema-version": "1.0.0",
    "builds": [
        {
            "id": "32.20201030.dev.0",
            "arches": [
                "x86_64"
            ]
        }
    ],
    "timestamp": "2020-10-30T16:45:21Z"
}
`

func TestBuildsMeta(t *testing.T) {
	tmpd, _ := ioutil.TempDir("", "buildjson")
	defer os.RemoveAll(tmpd)
	_ = os.MkdirAll(filepath.Join(tmpd, "builds"), 0755)

	bjson := filepath.Join(tmpd, CosaBuildsJSON)
	if err := ioutil.WriteFile(bjson, []byte(testData), 0666); err != nil {
		t.Fatalf("failed to write the test data %v", err)
	}

	b, err := getBuilds(tmpd)
	if err != nil {
		t.Fatalf("failed to find the builds")
	}
	if b == nil {
		t.Fatalf("builds should not be nil")
	}

	latest, ok := b.getLatest("x86_64")
	if !ok {
		t.Fatalf("x86_64 build should be available")
	}
	if latest != "32.20201030.dev.0" {
		t.Fatalf("build ID does not match")
	}
}

func TestCanArtifact(t *testing.T) {
	if !CanArtifact("aws") {
		t.Errorf("should be able to build AWS")
	}
	if CanArtifact("darkCloud") {
		t.Errorf("darkCloud is not a valid cloud")
	}
}

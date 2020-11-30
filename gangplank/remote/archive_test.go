package remote

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestCreateArchive(t *testing.T) {
	//create dirs/files
	tmpd, _ := ioutil.TempDir("", "teststages")
	defer os.RemoveAll(tmpd)

	//checkFunc := func() error { return nil }
	files := []struct {
		filename  string
		content   string
	}{
		{"builds.json", "{\n    \"schema-version\": \"1.0.0\",\n    \"timestamp\": \"2020-11-13T18:21:56Z\"\n}"},
		{"readme.md", "Test create tar ball function."},
		{"install.sh", "#!/usr/bin/bash\necho \"hello test\""},
		{"blank", ""},
	}

	for _, file := range files {
		tmpfile, err := ioutil.TempFile(file.filename, "")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpfile.Name())
	}

	//call createArchive to create tar ball

	//verify tar ball

}
package ocp

import (
	"context"
	"io/ioutil"
	"os"
	"testing"
)

const testDataFile = "build.json"

var testCtx = context.Background()

func init() {
	cosaSrvDir, _ = os.Getwd()
}

func TestOCPBuild(t *testing.T) {
	bData, err := ioutil.ReadFile(testDataFile)
	if err != nil {
		t.Errorf("failed to read %s: %v", testDataFile, err)
	}

	env := map[string]string{
		"BUILD":     string(bData),
		"COSA_CMDS": "cosa init",
	}
	for k, v := range env {
		os.Setenv(k, v)
	}
	defer func() {
		for k := range env {
			os.Unsetenv(k)
		}
	}()

	newO, err := NewBuilder(testCtx)
	if err != nil {
		t.Errorf("failed to read OCP envvars: %v", err)
	}
	if newO == nil {
		t.Errorf("failed to get API build")
	} else if newO.CosaCmds != "cosa init" {
		t.Errorf("cosa commands not set")
	}
}

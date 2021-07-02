package ocp

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
)

const testDataFile = "build.json"

func TestOCPBuild(t *testing.T) {
	tmpd, _ := ioutil.TempDir("", "test")
	defer os.RemoveAll(tmpd)
	cosaSrvDir = tmpd

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

	c := Cluster{inCluster: false}
	newO, err := newBC(context.Background(), &c)
	if err != nil {
		t.Errorf("failed to read OCP envvars: %v", err)
	}
	if newO == nil {
		t.Errorf("failed to get API build")
	} else if newO.CosaCmds != "cosa init" {
		t.Errorf("cosa commands not set")
	}
}

func TestGetPushTagless(t *testing.T) {
	tests := []struct {
		in       string
		registry string
		path     string
	}{
		{
			in:       "registry.foo:500/bar/baz/bin:tagged",
			registry: "registry.foo:500",
			path:     "bar/baz/bin",
		},
		{
			in:       "registry.foo/bar/baz/bin:tagged",
			registry: "registry.foo",
			path:     "bar/baz/bin",
		},
	}
	for idx, v := range tests {
		t.Run(fmt.Sprintf("test-%d", idx), func(t *testing.T) {
			reg, path := getPushTagless(v.in)
			if reg != v.registry {
				t.Errorf("registry:\n   got: %s\n  want: %s\n", reg, v.registry)
			}
			if path != v.path {
				t.Errorf("registry:\n   got: %s\n  want: %s\n", path, v.path)
			}
		})

	}
}

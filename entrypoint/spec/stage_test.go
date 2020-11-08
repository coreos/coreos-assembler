package spec

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// MockStageYaml is used to test inline specification
var MockStageYaml = fmt.Sprintf(`
%s
stages:
  - description: Test Stage
    commands:
       - echo {{ .Recipe.GitRef }}
       - echo {{ .Job.BuildName }}
  - description: Concurrent Stage Test
    concurrent: true
    prep_commands:
     - touch prep
    commands:
     - touch cmds
     - |
         bash -c 'echo this is multiline; 
                  echo test using inline yaml'
    post_commands:
     - test -f prep
     - test -f cmds
`, MockOSJobSpec)

func TestStages(t *testing.T) {
	tmpd, _ := ioutil.TempDir("", "teststages")
	defer os.RemoveAll(tmpd)

	checkFunc := func() error { return nil }
	tCases := []struct {
		desc      string
		wantErr   bool
		stages    []Stage
		checkFunc func() error
	}{
		{
			desc:      "Test Single Stage",
			checkFunc: checkFunc,
			wantErr:   false,
			stages: []Stage{
				{
					Description: "Single should pass",
					Commands:    []string{"echo hello"},
				},
			},
		},
		{
			desc:      "Test Dual Stage",
			checkFunc: checkFunc,
			wantErr:   false,
			stages: []Stage{
				{
					Description: "Dual single command should pass",
					Commands:    []string{"echo hello"},
				},
				{
					Description: "Dual concurrent should pass",
					Commands: []string{
						"echo {{ .Job.BuildName }}",
						"echo {{ .Recipe.GitRef }}",
					},
					ConcurrentExecution: true,
				},
			},
		},
		{
			desc:      "Test Bad Template",
			checkFunc: checkFunc,
			wantErr:   true,
			stages: []Stage{
				{
					Description: "Bad Template should fail",
					Commands:    []string{"echo {{ .This.Wont.Work }}"},
				},
			},
		},
		{
			desc:    "Test Bad Concurrent Template",
			wantErr: true,
			stages: []Stage{
				{
					Description:         "One command should fail",
					ConcurrentExecution: true,
					Commands: []string{
						"/bin/false",
						"/bin/true",
						"bob",
						fmt.Sprintf("/bin/sleep 3; touch %s/check", tmpd),
					},
				},
			},
			checkFunc: func() error {
				if _, err := os.Open(filepath.Join(tmpd, "check")); err != nil {
					return fmt.Errorf("check file is missing: %w", err)
				}
				return nil
			},
		},
		{
			desc:    "Test Prep and Post",
			wantErr: false,
			stages: []Stage{
				{
					Description:         "Check command ordering",
					ConcurrentExecution: true,
					PrepCommands: []string{
						fmt.Sprintf("touch %s/prep", tmpd),
					},
					Commands: []string{
						fmt.Sprintf("test -f %s/prep", tmpd),
						fmt.Sprintf("touch %s/commands", tmpd),
					},
					PostCommands: []string{
						fmt.Sprintf("test -f %s/commands", tmpd),
						fmt.Sprintf("touch %s/post", tmpd),
					},
				},
			},
			checkFunc: func() error {
				for _, c := range []string{"prep", "commands", "post"} {
					if _, err := os.Stat(filepath.Join(tmpd, c)); err != nil {
						return fmt.Errorf("check file is missing: %w", err)
					}
				}
				return nil
			},
		},
	}

	testEnv := []string{
		"MOCK_ENV=1",
		"TEST_VAR=2",
	}

	js := JobSpec{}
	if err := yaml.Unmarshal([]byte(MockOSJobSpec), &js); err != nil {
		t.Errorf("failed to read mock jobspec")
	}
	ctx := context.Background()

	for _, c := range tCases {
		t.Logf(" * %s ", c.desc)
		for _, stage := range c.stages {
			t.Logf("  - test name: %s", stage.Description)
			err := stage.Execute(ctx, &js, testEnv)
			if c.wantErr && err == nil {
				t.Error("    SHOULD error, but did not")
			}
			if err != nil && !c.wantErr {
				t.Errorf("    SHOULD NOT error, but did: %v", err)
			}
			if err = c.checkFunc(); err != nil {
				t.Errorf("    %v", err)
			}
		}
	}
}

func TestStageYaml(t *testing.T) {
	myD, _ := os.Getwd()
	defer os.Chdir(myD) //nolint
	tmpd, _ := ioutil.TempDir("", "stagetest")
	_ = os.Chdir(tmpd)
	defer os.RemoveAll(tmpd)

	r := strings.NewReader(MockStageYaml)
	js, err := JobSpecReader(r)
	if err != nil {
		t.Fatalf("failed to get jobspec: %v", err)
	}
	c, cancel := context.WithCancel(context.Background())
	defer c.Done()
	defer cancel()

	for _, stage := range js.Stages {
		t.Logf("* executing stage: %s", stage.Description)
		if err := stage.Execute(c, &js, []string{}); err != nil {
			t.Errorf("failed inline stage execution: %v", err)
		}
	}
}

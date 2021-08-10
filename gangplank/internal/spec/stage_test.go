package spec

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coreos/coreos-assembler-schema/cosa"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

// MockStageYaml is used to test inline specification
var MockStageYaml = fmt.Sprintf(`
%s
stages:
  - description: Test Stage
    commands:
       - echo {{ .JobSpec.Recipe.GitRef }}
       - echo {{ .JobSpec.Job.BuildName }}
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

	rd := &RenderData{
		JobSpec: new(JobSpec),
		Meta:    new(cosa.Build),
	}
	rd.Meta.BuildID = "MockBuild"
	rd.Meta.Architecture = "ARMv6"

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
						"echo {{ .JobSpec.Job.BuildName }}",
						"echo {{ .JobSpec.Recipe.GitRef }}",
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
		{
			desc:    "Test Templating",
			wantErr: false,
			stages: []Stage{
				{
					Description: "Templating check",
					Commands: []string{
						fmt.Sprintf("touch %s/{{.Meta.BuildID}}.{{.Meta.Architecture}}", tmpd),
					},
				},
			},
			checkFunc: func() error {
				_, err := os.Stat(filepath.Join(tmpd, fmt.Sprintf("%s.%s", rd.Meta.BuildID, rd.Meta.Architecture)))
				return err
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
			err := stage.Execute(ctx, rd, testEnv)
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

	rd := &RenderData{
		JobSpec: &js,
		Meta:    nil,
	}

	c, cancel := context.WithCancel(context.Background())
	defer c.Done()
	defer cancel()

	for _, stage := range js.Stages {
		t.Logf("* executing stage: %s", stage.Description)
		if err := stage.Execute(c, rd, []string{}); err != nil {
			t.Errorf("failed inline stage execution: %v", err)
		}
	}
}

func TestIsArtifactValid(t *testing.T) {
	testCases := []struct {
		artifact string
		want     bool
	}{
		{
			artifact: "base",
			want:     true,
		},
		{
			artifact: "AWS",
			want:     true,
		},
		{
			artifact: "finalize",
			want:     true,
		},
		{
			artifact: "mandrake+root",
			want:     false,
		},
	}
	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("test-%d-%s", idx, tc.artifact), func(t *testing.T) {
			got := isValidArtifactShortHand(tc.artifact)
			if got != tc.want {
				t.Errorf("artifact %s should return %v but got %v", tc.artifact, tc.want, got)
			}
		})
	}
}

func TestBuildCommandOrders(t *testing.T) {
	type testCase struct {
		desc       string
		shorthands []string
		stage      *Stage
		want       *Stage
		testFn     func(t *testing.T)
	}

	testCases := []testCase{}
	testCases = append(testCases,
		func() testCase {
			// Test that base returns base
			tc := testCase{
				desc:       "base shorthand is understood",
				shorthands: []string{"base"},
				stage: &Stage{
					BuildArtifacts: []string{"base"},
				},
				want: &Stage{
					BuildArtifacts: []string{"base"},
				},
			}
			tc.testFn = func(t *testing.T) {
				addAllShorthandsToStage(tc.stage, tc.shorthands...)
				assert.Len(t, tc.stage.BuildArtifacts, 1)
				for _, v := range tc.want.BuildArtifacts {
					assert.Contains(t, tc.stage.BuildArtifacts, v)
				}
			}
			return tc
		}(),

		// Ensure that base implies qemu for aws
		func() testCase {
			tc := testCase{
				desc:       "base should build before aws",
				shorthands: []string{"aws"},
				stage: &Stage{
					BuildArtifacts: []string{"base"},
				},
				want: &Stage{
					BuildArtifacts: []string{"base", "aws"},
				},
			}

			tc.testFn = func(t *testing.T) {
				addAllShorthandsToStage(tc.stage, tc.shorthands...)
				assert.Equal(t, tc.want.BuildArtifacts, tc.stage.BuildArtifacts)
				for idx, v := range tc.stage.BuildArtifacts {
					if idx == 0 {
						assert.Equal(t, "base", v, "base should be ordered before aws")
					}
					if idx == 1 {
						assert.Equal(t, "aws", v, "aws should be ordered after base")
					}
				}
			}
			return tc
		}(),

		// Base implies ostree and qemu
		func() testCase {
			tc := testCase{
				desc:       "base implies ostree and qemu",
				shorthands: []string{"ostree", "qemu"},
				stage: &Stage{
					BuildArtifacts: []string{"base"},
				},
				want: &Stage{
					BuildArtifacts: []string{"base"},
				},
			}
			tc.testFn = func(t *testing.T) {
				addAllShorthandsToStage(tc.stage, tc.shorthands...)
				assert.Equal(t, tc.want.BuildArtifacts, tc.stage.BuildArtifacts)
			}
			return tc
		}(),
		/*
			func() testCase {
				tc := testCase{}
				tc.testFn = func(t *testing.T) {
					addAllShorthandsToStage(tc.stage, tc.shorthands...)
				}
				return tc
			}(),
		*/
	)

	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("test-%d-%v", idx, tc.desc), tc.testFn)
	}
}

func TestBuildCommands(t *testing.T) {
	testCases := []struct {
		desc     string
		artifact string
		want     []string
		js       *JobSpec
	}{
		// Ensure that `cosa build X` commands are rendered properly.
		{
			desc:     "base command should be 'cosa build'",
			artifact: "base",
			want:     []string{fmt.Sprintf(defaultBaseCommand, "")},
			js:       &JobSpec{DelayedMetaMerge: false},
		},
		{
			desc:     "ostree command should be 'cosa build ostree'",
			artifact: "ostree",
			want:     []string{fmt.Sprintf(defaultBaseCommand, "ostree")},
			js:       &JobSpec{DelayedMetaMerge: false},
		},
		{
			desc:     "qemu command should be 'cosa build qemu'",
			artifact: "qemu",
			want:     []string{fmt.Sprintf(defaultBaseCommand, "qemu")},
			js:       &JobSpec{DelayedMetaMerge: false},
		},

		// Ensure that `cosa build --delay-merge X` commands are rendered properly.
		{
			desc:     "base command should be 'cosa build --delay-merge'",
			artifact: "base",
			want:     []string{fmt.Sprintf(defaultBaseDelayMergeCommand, "")},
			js:       &JobSpec{DelayedMetaMerge: true},
		},
		{
			desc:     "ostree command should be 'cosa build --delay-merge ostree'",
			artifact: "ostree",
			want:     []string{fmt.Sprintf(defaultBaseDelayMergeCommand, "ostree")},
			js:       &JobSpec{DelayedMetaMerge: true},
		},
		{
			desc:     "qemu command should be 'cosa build --delay-merge qemu'",
			artifact: "qemu",
			want:     []string{fmt.Sprintf(defaultBaseDelayMergeCommand, "qemu")},
			js:       &JobSpec{DelayedMetaMerge: true},
		},

		// Check finalize
		{
			desc:     "finalize command should match",
			artifact: "finalize",
			want:     []string{defaultFinalizeCommand},
			js:       &JobSpec{DelayedMetaMerge: true},
		},
	}

	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("test-%d-%s", idx, tc.desc), func(t *testing.T) {
			cmd, _ := cosaBuildCmd(tc.artifact, tc.js)
			assert.EqualValues(t, tc.want, cmd)
		})
	}
}

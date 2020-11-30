// Copyright 2017 CoreOS, Inc.
// Copyright 2014 The Go Authors.
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

package mockssh

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestExitStatusZero(t *testing.T) {
	client := NewMockClient(func(s *Session) {
		if err := s.Exit(0); err != nil {
			t.Error(err)
		}
	})
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	if err := session.Run(""); err != nil {
		t.Fatal(err)
	}
}

func TestExitStatusNonzero(t *testing.T) {
	client := NewMockClient(func(s *Session) {
		if err := s.Exit(42); err != nil {
			t.Error(err)
		}
	})
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	err = session.Run("")
	if ex, ok := err.(*ssh.ExitError); !ok {
		t.Fatalf("unexpected error: %v", err)
	} else if ex.ExitStatus() != 42 {
		t.Fatalf("unexpected exit: %v", err)
	}
}

func TestExitStatusMissing(t *testing.T) {
	client := NewMockClient(func(s *Session) {
		s.Close()
	})
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	err = session.Run("")
	if _, ok := err.(*ssh.ExitMissingError); !ok {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCat(t *testing.T) {
	client := NewMockClient(func(s *Session) {
		if _, err := io.Copy(s.Stdout, s.Stdin); err != nil {
			t.Error(err)
		}
		if err := s.Exit(0); err != nil {
			t.Error(err)
		}
	})
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	const data = "hello world\n"
	session.Stdin = strings.NewReader("hello world\n")

	out, err := session.Output("")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != data {
		t.Fatalf("unexpected output: %q; wanted %q", out, data)
	}
}

func TestStderr(t *testing.T) {
	client := NewMockClient(func(s *Session) {
		if _, err := io.WriteString(s.Stdout, "Stdout"); err != nil {
			t.Error(err)
		}
		if _, err := io.WriteString(s.Stderr, "Stderr"); err != nil {
			t.Error(err)
		}
		if err := s.Exit(0); err != nil {
			t.Error(err)
		}
	})
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(""); err != nil {
		t.Fatal(err)
	}

	if stdout.String() != "Stdout" {
		t.Errorf("got %q wanted %q", stdout.String(), "Stdout")
	}
	if stderr.String() != "Stderr" {
		t.Errorf("got %q wanted %q", stderr.String(), "Stderr")
	}
}

func TestExec(t *testing.T) {
	const cmd = "test command"
	client := NewMockClient(func(s *Session) {
		if s.Exec != cmd {
			t.Errorf("got %q wanted %q", s.Exec, cmd)
		}
		if err := s.Exit(0); err != nil {
			t.Error(err)
		}
	})
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	if err := session.Run(cmd); err != nil {
		t.Fatal(err)
	}
}

func TestEnv(t *testing.T) {
	expect := []string{"VAR1=VALUE1", "VAR2=VALUE2"}
	client := NewMockClient(func(s *Session) {
		if !reflect.DeepEqual(s.Env, expect) {
			t.Errorf("got %v wanted %v", s.Env, expect)
		}
		if err := s.Exit(0); err != nil {
			t.Error(err)
		}
	})
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	if err := session.Setenv("VAR1", "VALUE1"); err != nil {
		t.Fatal(err)
	}

	if err := session.Setenv("VAR2", "VALUE2"); err != nil {
		t.Fatal(err)
	}

	if err := session.Run(""); err != nil {
		t.Fatal(err)
	}
}

// shell is not implemented, confirm it fails.
func TestShell(t *testing.T) {
	client := NewMockClient(func(s *Session) {
		t.Errorf("executed shell")
		if err := s.Exit(0); err != nil {
			t.Error(err)
		}
	})
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}

	if err := session.Shell(); err == nil {
		t.Fatal("shell succeeded")
	}
}

// The server code spawns a lot of goroutines and all of them need to
// gracefully terminate when the client is closed.
func TestMain(m *testing.M) {
	g0 := runtime.NumGoroutine()

	code := m.Run()
	if code != 0 {
		os.Exit(code)
	}

	// Check that there are no goroutines left behind.
	t0 := time.Now()
	stacks := make([]byte, 1<<20)
	for {
		g1 := runtime.NumGoroutine()
		if g1 == g0 {
			return
		}
		stacks = stacks[:runtime.Stack(stacks, true)]
		time.Sleep(50 * time.Millisecond)
		if time.Since(t0) > 2*time.Second {
			fmt.Fprintf(os.Stderr, "Unexpected leftover goroutines detected: %v -> %v\n%s\n", g0, g1, stacks)
			os.Exit(1)
		}
	}
}

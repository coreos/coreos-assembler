package spec

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
)

// executeTemplate applies the template to r.
func (j *JobSpec) executeTemplate(r io.Reader) ([]byte, error) {
	var in bytes.Buffer
	if _, err := in.ReadFrom(r); err != nil {
		return nil, err
	}
	tmpl, err := template.New("args").Parse(in.String())
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	err = tmpl.Execute(&out, j)
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ExecuteTemplateFromString returns strings.
func (j *JobSpec) ExecuteTemplateFromString(s ...string) ([]string, error) {
	var ret []string
	for _, x := range s {
		r := strings.NewReader(x)
		b, err := j.executeTemplate(r)
		if err != nil {
			return nil, fmt.Errorf("failed to render strings: %v", err)
		}
		ret = append(ret, string(b))
	}
	return ret, nil
}

// ExecuteTemplateToWriter renders an io.Reader to an io.Writer.
func (j *JobSpec) ExecuteTemplateToWriter(in io.Reader, out io.Writer) error {
	d, err := j.executeTemplate(in)
	if err != nil {
		return err
	}
	if _, err := out.Write(d); err != nil {
		return err
	}
	return nil
}

// RendererExecuter renders a script with templates and then executes it
func (j *JobSpec) RendererExecuter(ctx context.Context, env []string, scripts ...string) error {
	rendered := make(map[string]*os.File)
	for _, script := range scripts {
		in, err := os.Open(script)
		if err != nil {
			return err
		}
		t, err := ioutil.TempFile("", "rendered")
		if err != nil {
			return err
		}
		defer os.Remove(t.Name())
		rendered[script] = t

		if err := j.ExecuteTemplateToWriter(in, t); err != nil {
			return err
		}
	}

	for i, v := range rendered {
		cArgs := []string{"-xeu", "-o", "pipefail", v.Name()}
		log.WithFields(log.Fields{
			"cmd":      "/bin/bash",
			"rendered": v.Name(),
		}).Info("Executing rendered script")
		cmd := exec.CommandContext(ctx, "/bin/bash", cArgs...)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Script exited with return code %v", err)
		}
		log.WithFields(log.Fields{"script": i}).Info("Script complete")
	}
	return nil
}

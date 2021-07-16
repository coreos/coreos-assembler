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

	"github.com/coreos/coreos-assembler-schema/cosa"
	log "github.com/sirupsen/logrus"
)

// RenderData is used to render commands
type RenderData struct {
	JobSpec *JobSpec
	Meta    *cosa.Build
}

// executeTemplate applies the template to r.
func (rd *RenderData) executeTemplate(r io.Reader) ([]byte, error) {
	var in bytes.Buffer
	if _, err := in.ReadFrom(r); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	tmpl, err := template.New("args").Parse(in.String())
	if err != nil {
		return nil, err
	}

	err = tmpl.Execute(&out, rd)
	if err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

// ExecuteTemplateFromString returns strings.
func (rd *RenderData) ExecuteTemplateFromString(s ...string) ([]string, error) {
	var ret []string
	for _, x := range s {
		r := strings.NewReader(x)
		b, err := rd.executeTemplate(r)
		if err != nil {
			return nil, fmt.Errorf("failed to render strings: %v", err)
		}
		ret = append(ret, string(b))
	}
	return ret, nil
}

// ExecuteTemplateToWriter renders an io.Reader to an io.Writer.
func (rd *RenderData) ExecuteTemplateToWriter(in io.Reader, out io.Writer) error {
	d, err := rd.executeTemplate(in)
	if err != nil {
		return err
	}
	if _, err := out.Write(d); err != nil {
		return err
	}
	return nil
}

// RendererExecuter renders a script with templates and then executes it
func (rd *RenderData) RendererExecuter(ctx context.Context, env []string, scripts ...string) error {
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

		if err := rd.ExecuteTemplateToWriter(in, t); err != nil {
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

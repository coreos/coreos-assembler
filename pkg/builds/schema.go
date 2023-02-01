package builds

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pkg/errors"

	schema "github.com/xeipuuv/gojsonschema"
)

var (
	// SchemaJSON Schema document. Default the generated Schema.
	SchemaJSON = generatedSchemaJSON
)

func init() {
	runtimeSchemaPath := os.Getenv("COSA_META_SCHEMA")
	if strings.ToLower(runtimeSchemaPath) == "none" {
		return
	}
	if runtimeSchemaPath != "" {
		f, err := os.Open(runtimeSchemaPath)
		if err != nil {
			panic(errors.Wrapf(err, "failed to open schema file %s", runtimeSchemaPath))
		}
		defer f.Close()

		if err := SetSchemaFromFile(f); err != nil {
			panic(errors.Wrapf(err, "failed to read in schema file %s", runtimeSchemaPath))
		}
	}
}

// SetSchemaFromFile sets the validation JSON Schema
func SetSchemaFromFile(r io.Reader) error {
	if r == nil {
		return errors.New("schema input is invalid")
	}
	in, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	SchemaJSON = string(in)
	return nil
}

// Validate checks the build against the schema.
func (build *Build) Validate() []error {
	var e []error
	data, err := json.Marshal(build)
	if err != nil {
		return append(e, err)
	}
	if len(data) == 0 {
		return append(e,
			errors.New("build data is empty"),
		)
	}

	result, _ := schema.Validate(
		schema.NewStringLoader(SchemaJSON),
		schema.NewStringLoader(string(data)),
	)

	if result.Valid() {
		return nil
	}

	for _, desc := range result.Errors() {
		e = append(e, fmt.Errorf("invalid: %s", desc))
	}

	return e
}

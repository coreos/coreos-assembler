# schematyper

Generates Go struct types based on a [JSON Schema](http://json-schema.org/).

## Installation
```
$ go get github.com/idubinskiy/schematyper
```

## Usage
```
$ schematyper schema.json
```
Creates a `schema_schematype.go` file with package `main`.

Command line options:
```
usage: schematyper [<flags>] <input>

Flags:
      --help                 Show context-sensitive help (also try --help-long and --help-man).
  -c, --console              output to console instead of file
  -o, --out-file=OUT-FILE    filename for output; default is <schema>_schematype.go
      --package="main"       package name for generated file; default is "main"
      --root-type=ROOT-TYPE  name of root type; default is generated from the filename
      --prefix=PREFIX        prefix for non-root types
      --ptr-for-omit         use a pointer to a struct for an object
                             property that is represented as a struct if the property is not required (i.e., has omitempty tag)

Args:
  <input>  file containing a valid JSON schema
```

`package main` (the default) will generate unexported types. Any other package name defaults to exported types. `--root-type` and `--prefix` can be used to override this behavior.

Can be used with [`go generate`](https://blog.golang.org/generate):
```go
//go:generate schematyper -o schema_type.go -package mypackage schemas/schema.json
```

## Schema Features Support
Supports the following JSON Schema keywords:
* `title` - sets type name
* `description` - sets type comment
* `required` - sets which fields in type don't have `omitempty`. If --ptr-for-omit is specified and the field is not required, a field that is an object represented as a struct is generated as a pointer to the struct.
* `properties` - determines struct fields
* `additionalProperties` - determines struct type of map values
* `type` - sets field type (`string`, `bool`, etc.). Examples:
    * `["string", "null"]` sets `*string`
    * `"object"` sets `map[string]interface{}`, `map[string]<new type>`, or a new struct type depending on schema
    * `"array"` sets `[]interface{}` or `[]<new type>` depending on schema
    * `["string", "integer"]` sets `interface{}`
* `items` - sets array items type, similar to `type`
* `format` - if `date-time`, sets type to `time.Time` and imports `time`
* `definitions` - creates additional types which can be referenced using `$ref`
* `$ref` - Reference a local schema (same file).

Support for more features is pending, but many will require adding run-time checks by implementing the `json.Marshaler` and `json.Unmarshaler` interfaces.

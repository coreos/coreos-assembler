package index

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"

	storage "code.google.com/p/google-api-go-client/storage/v1"
)

var indexTemplate *template.Template

func init() {
	indexText := `<html>
    <head>
	<title>{{.Bucket}}/{{.Prefix}}</title>
    </head>
    <body>
    <h1>{{.Bucket}}/{{.Prefix}}</h1>
    {{range $name, $sub := .SubDirs}}
	[dir] <a href="{{$name}}">{{$name}}</a> </br>
    {{end}}
    {{range $name, $obj := .Objects}}
	{{if ne $name "index.html"}}
	    [file] <a href="{{$name}}">{{$name}}</a> </br>
	{{end}}
    {{end}}
    </body>
</html>
`
	indexTemplate = template.Must(template.New("index").Parse(indexText))
}

func (d *Directory) WriteIndex(client *http.Client) error {
	service, err := storage.New(client)
	if err != nil {
		return err
	}

	buf := bytes.Buffer{}
	err = indexTemplate.Execute(&buf, d)
	if err != nil {
		return err
	}

	writeObj := storage.Object{
		Name:        d.Prefix + "index.html",
		ContentType: "text/html",
	}
	writeReq := service.Objects.Insert(d.Bucket, &writeObj)
	writeReq.Media(&buf)

	fmt.Printf("Writing gs://%s/%s\n", d.Bucket, writeObj.Name)
	_, err = writeReq.Do()
	return err
}

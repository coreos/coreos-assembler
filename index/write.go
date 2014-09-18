package index

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"code.google.com/p/google-api-go-client/googleapi"
	"code.google.com/p/google-api-go-client/storage/v1"
)

var (
	// Retry write requests up to 6 times.
	maxTries int = 6
	// Wait no less than a second before retrying.
	minBackoff time.Duration = time.Second
	// Do not wait more than 8 seconds between tries.
	maxBackoff time.Duration = time.Second * 8

	indexTemplate *template.Template
)

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

func expBackoff(interval time.Duration) time.Duration {
	interval = interval * 2
	if interval > maxBackoff {
		interval = maxBackoff
	}
	return interval
}

func serverError(err error) bool {
	if apierr, ok := err.(*googleapi.Error); ok {
		if apierr.Code == 500 || apierr.Code == 503 {
			return true
		}
	}

	return false
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

	// Retry write, sometimes transient 500 errors are reported.
	retryDelay := minBackoff
	for try := 1; try <= maxTries; try++ {
		_, err = writeReq.Do()
		if err != nil && serverError(err) {
			time.Sleep(retryDelay)
			retryDelay = expBackoff(retryDelay)
		} else {
			break
		}
	}
	return err
}

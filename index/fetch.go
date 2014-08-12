package index

import (
	"fmt"
	"net/http"
	"strings"

	storage "code.google.com/p/google-api-go-client/storage/v1"
)

func (d *Directory) Fetch(client *http.Client) error {
	service, err := storage.New(client)
	if err != nil {
		return err
	}

	fmt.Printf("Fetching gs://%s/%s\n", d.Bucket, d.Prefix)
	objCount := 0
	listReq := service.Objects.List(d.Bucket)
	if d.Prefix != "" {
		listReq.Prefix(d.Prefix)
	}

	for {
		objs, err := listReq.Do()
		if err != nil {
			return err
		}

		objCount += len(objs.Items)
		fmt.Printf("Found %d objects under gs://%s/%s\n", objCount, d.Bucket, d.Prefix)
		for _, obj := range objs.Items {
			relativeName := strings.TrimPrefix(obj.Name, d.Prefix)
			d.AddObject(relativeName, obj)
		}

		if objs.NextPageToken != "" {
			listReq.PageToken(objs.NextPageToken)
		} else {
			break
		}
	}

	return nil
}

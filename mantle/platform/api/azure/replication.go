// Copyright 2016 CoreOS, Inc.
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

package azure

import (
	"encoding/xml"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/management/location"
)

const (
	computeService = "Compute"
)

var (
	azureImageReplicateURL   = "services/images/%s/replicate"
	azureImageUnreplicateURL = "services/images/%s/unreplicate"
)

type ReplicationInput struct {
	XMLName         xml.Name `xml:"http://schemas.microsoft.com/windowsazure ReplicationInput"`
	TargetLocations []string `xml:"TargetLocations>Region"`
	Offer           string   `xml:"ComputeImageAttributes>Offer"`
	Sku             string   `xml:"ComputeImageAttributes>Sku"`
	Version         string   `xml:"ComputeImageAttributes>Version"`
}

// Locations returns a slice of Azure Locations which offer the Compute
// service, useful for replicating to all Locations.
func (a *API) Locations() ([]string, error) {
	lc := location.NewClient(a.client)

	llr, err := lc.ListLocations()
	if err != nil {
		return nil, err
	}

	var locations []string

	for _, l := range llr.Locations {
		haveCompute := false
		for _, svc := range l.AvailableServices {
			if svc == computeService {
				haveCompute = true
				break
			}
		}

		if haveCompute {
			locations = append(locations, l.Name)
		} else {
			plog.Infof("Skipping location %q without %s service", l.Name, computeService)
		}
	}

	return locations, nil
}

func (a *API) ReplicateImage(image, offer, sku, version string, regions ...string) error {
	ri := ReplicationInput{
		TargetLocations: regions,
		Offer:           offer,
		Sku:             sku,
		Version:         version,
	}

	data, err := xml.Marshal(&ri)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(azureImageReplicateURL, image)

	op, err := a.client.SendAzurePutRequest(url, "", data)
	if err != nil {
		return err
	}

	return a.client.WaitForOperation(op, nil)
}

func (a *API) UnreplicateImage(image string) error {
	url := fmt.Sprintf(azureImageUnreplicateURL, image)
	op, err := a.client.SendAzurePutRequest(url, "", []byte{})
	if err != nil {
		return err
	}

	return a.client.WaitForOperation(op, nil)
}

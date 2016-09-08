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

// +build go1.7

package azure

import (
	"encoding/xml"

	"github.com/Azure/azure-sdk-for-go/management/storageservice"
)

var (
	azureImageURL = "services/images"
)

func (a *API) GetStorageServiceKeys(account string) (storageservice.GetStorageServiceKeysResponse, error) {
	return storageservice.NewClient(a.client).GetStorageServiceKeys(account)
}

// https://msdn.microsoft.com/en-us/library/azure/jj157192.aspx
func (a *API) AddOSImage(md *OSImage) error {
	data, err := xml.Marshal(md)
	if err != nil {
		return err
	}

	op, err := a.client.SendAzurePostRequest(azureImageURL, data)
	if err != nil {
		return err
	}

	return a.client.WaitForOperation(op, nil)
}

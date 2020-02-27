// Copyright 2018 CoreOS, Inc.
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
	"time"

	"github.com/Azure/azure-sdk-for-go/arm/resources/resources"

	"github.com/coreos/mantle/util"
)

func (a *API) CreateResourceGroup(prefix string) (string, error) {
	name := randomName(prefix)
	tags := map[string]*string{
		"createdAt": util.StrToPtr(time.Now().Format(time.RFC3339)),
		"createdBy": util.StrToPtr("mantle"),
	}

	_, err := a.rgClient.CreateOrUpdate(name, resources.Group{
		Location: &a.opts.Location,
		Tags:     &tags,
	})
	if err != nil {
		return "", err
	}

	return name, nil
}

func (a *API) TerminateResourceGroup(name string) error {
	resp, err := a.rgClient.CheckExistence(name)
	if err != nil {
		return err
	}
	if resp.StatusCode != 204 {
		return nil
	}

	_, err = a.rgClient.Delete(name, nil)
	return err
}

func (a *API) ListResourceGroups(filter string) (resources.GroupListResult, error) {
	return a.rgClient.List(filter, nil)
}

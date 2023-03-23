// Copyright 2023 Red Hat
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
	"context"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

func (a *API) CreateResourceGroup(prefix string) (string, error) {
	name := randomName(prefix)
	tags := map[string]*string{
		"createdAt": to.Ptr(time.Now().Format(time.RFC3339)),
		"createdBy": to.Ptr("mantle"),
	}

	_, err := a.rgClient.CreateOrUpdate(context.Background(), name, armresources.ResourceGroup{
		Location: to.Ptr(a.opts.Location),
		Tags:     tags,
	}, nil)
	if err != nil {
		return "", err
	}

	return name, nil
}

func (a *API) TerminateResourceGroup(name string) error {
	resp, err := a.rgClient.CheckExistence(context.Background(), name, nil)
	if err != nil {
		return err
	}
	if !resp.Success {
		return nil
	}

	// Request the delete and wait until the resource group is cleaned up.
	ctx := context.Background()
	poller, err := a.rgClient.BeginDelete(ctx, name, nil)
	if err != nil {
		return err
	}

	_, err = poller.PollUntilDone(ctx, nil)

	return err
}

func (a *API) ListResourceGroups() ([]*armresources.ResourceGroup, error) {
	ctx := context.Background()

	resultPager := a.rgClient.NewListPager(nil)

	resourceGroups := make([]*armresources.ResourceGroup, 0)
	for resultPager.More() {
		pageResp, err := resultPager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		resourceGroups = append(resourceGroups, pageResp.ResourceGroupListResult.Value...)
	}
	return resourceGroups, nil
}

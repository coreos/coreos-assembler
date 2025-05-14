// Copyright 2025 Red Hat
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
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"

	"github.com/coreos/coreos-assembler/mantle/util"
)

// CreateDisk provisions a new managed disk in the specified Azure resource group using
// the given name, size (in GiB), and SKU (e.g., Premium_LRS). The disk is created in
// the location and availability zone specified in the API options.
func (a *API) CreateDisk(name, resourceGroup string, sizeGB int32, sku armcompute.DiskStorageAccountTypes) (string, error) {
	ctx := context.Background()
	poller, err := a.diskClient.BeginCreateOrUpdate(ctx, resourceGroup, name, armcompute.Disk{
		Location: &a.opts.Location,
		Zones:    []*string{&a.opts.AvailabilityZone},
		Tags: map[string]*string{
			"createdBy": to.Ptr("mantle"),
		},
		SKU: &armcompute.DiskSKU{
			Name: to.Ptr(sku),
		},
		Properties: &armcompute.DiskProperties{
			DiskSizeGB: to.Ptr(sizeGB),
			CreationData: &armcompute.CreationData{
				CreateOption: to.Ptr(armcompute.DiskCreateOptionEmpty),
			},
		},
	}, nil)

	if err != nil {
		return "", fmt.Errorf("failed to create azure disk %v", err)
	}

	diskResponse, err := poller.PollUntilDone(context.Background(), nil)
	if err != nil {
		return "", err
	}

	if diskResponse.Disk.ID == nil {
		return "", fmt.Errorf("failed to get azure disk id")
	}

	return *diskResponse.Disk.ID, nil
}

// DeleteDisk deletes a managed disk by name from the specified Azure resource group.
func (a *API) DeleteDisk(name, resourceGroup string) error {
	ctx := context.Background()
	poller, err := a.diskClient.BeginDelete(ctx, resourceGroup, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

// ParseDisk parses a disk specification string from a kola test and returns the
// disk size and the Azure disk SKU. The spec format is "<size>:sku=<type>",
// e.g., ["10G:sku=UltraSSD_LRS"] for NVMe disks. If no SKU is specified, "Standard_LRS" is used.
func (a *API) ParseDisk(spec string) (int64, armcompute.DiskStorageAccountTypes, error) {
	sku := armcompute.DiskStorageAccountTypes(armcompute.DiskStorageAccountTypesStandardLRS)
	size, diskmap, err := util.ParseDiskSpec(spec, false)
	if err != nil {
		return size, sku, fmt.Errorf("failed to parse disk spec %q: %w", spec, err)
	}
	for key, value := range diskmap {
		switch key {
		case "sku":
			normalizedSku := strings.ToUpper(value)
			foundSku := false
			for _, validSku := range armcompute.PossibleDiskStorageAccountTypesValues() {
				if strings.EqualFold(normalizedSku, string(validSku)) {
					sku = validSku
					foundSku = true
					break
				}
			}
			if !foundSku {
				return size, sku, fmt.Errorf("unsupported disk sku: %s", value)
			}
		default:
			return size, sku, fmt.Errorf("invalid key: %s", key)
		}
	}
	return size, sku, nil
}

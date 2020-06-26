// Copyright 2020 Red Hat, Inc.
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

package aws

import (
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/service/marketplacecatalog"
)

type entityDetails struct {
	AmiRevisionArns []string
}

// AddAmiToMarketplace adds a new Entity of entityType containing the ARN of the new AMI
func (a *API) AddAmiToMarketplace(entityType, entityId, newAmi, newVersion string) (string, error) {
	// catalogName is hardcoded
	catalogName := "AWSMarketplace"

	newAmiArn := fmt.Sprintf("arn: aws:aws-marketplace:us-east-1:%s:%s/%s/%s", newAmi, catalogName, entityType, newVersion)
	newRevisionArns := make([]string, 1)
	newRevisionArns[0] = newAmiArn

	details := entityDetails{
		AmiRevisionArns: newRevisionArns,
	}
	detailsJSONData, err := json.Marshal(details)
	if err != nil {
		return "", fmt.Errorf("error marshalling change details: %v", err)
	}
	detailsString := string(detailsJSONData)

	entity := marketplacecatalog.Entity{
		Identifier: &entityId,
		Type:       &entityType, // "RHCOSIMG@1.0"
	}

	addAmiChangeType := "AddAmiArn"

	change := &marketplacecatalog.Change{
		ChangeType: &addAmiChangeType,
		Details:    &detailsString,
		Entity:     &entity,
	}
	changeSet := make([]*marketplacecatalog.Change, 1)
	changeSet[0] = change

	clientRequestToken := "add-new-release-ami-arn" + newAmiArn

	input := marketplacecatalog.StartChangeSetInput{
		Catalog:            &catalogName,
		ChangeSet:          changeSet,
		ClientRequestToken: &clientRequestToken,
	}

	_, err = a.marketplacecatalog.StartChangeSet(&input)
	if err != nil {
		return "", fmt.Errorf("error applying changes: %v", err)
	}

	describeEntityInput := marketplacecatalog.DescribeEntityInput{
		Catalog:  &catalogName,
		EntityId: &entityId,
	}

	entityDescription, err := a.marketplacecatalog.DescribeEntity(describeEntityInput)
	if err != nil {
		return "", fmt.Errorf("error getting entity metadata: %v", err)
	}

	return entityDescription.String(), nil
}

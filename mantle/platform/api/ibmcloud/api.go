// Copyright 2021 Red Hat
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

// Most of the functions here follow: https://github.com/ppc64le-cloud/pvsadm which is an implementation of
// tools to interact with the IBMCloud storage and also the Power Virtual Server.

package ibmcloud

import (
	"fmt"
	gohttp "net/http"

	"github.com/IBM-Cloud/bluemix-go"
	"github.com/IBM-Cloud/bluemix-go/api/resource/resourcev1/catalog"
	"github.com/IBM-Cloud/bluemix-go/api/resource/resourcev1/controller"
	"github.com/IBM-Cloud/bluemix-go/api/resource/resourcev1/management"
	"github.com/IBM-Cloud/bluemix-go/api/resource/resourcev2/controllerv2"
	"github.com/IBM-Cloud/bluemix-go/authentication"
	"github.com/IBM-Cloud/bluemix-go/http"
	"github.com/IBM-Cloud/bluemix-go/models"
	"github.com/IBM-Cloud/bluemix-go/rest"
	bluemixsession "github.com/IBM-Cloud/bluemix-go/session"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/pkg/capnslog"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/api/ibmcloud")

var (
	tokenProviderEndpoint                = "https://iam.cloud.ibm.com"
	defaultResourceGroupAPIRegion        = "global"
	defaultCloudObjectStorageServiceType = "cloud-object-storage"
	defaultCloudObjectStorageServicePlan = "standard"
)

type Options struct {
	*platform.Options
	// The path to the shared credentials file, if not ~/.bluemix/credentials
	CredentialsFile string

	// ApiKey is the optional access key to use. It will override all other sources
	ApiKey string

	// Cloud Object storage name to use
	CloudObjectStorage string
}

// Client used to interact with the IBMCloud apis
type Client struct {
	*bluemixsession.Session
	ResourceClientV2   controllerv2.ResourceServiceInstanceRepository
	ResourceClientV1   controller.ResourceServiceInstanceRepository
	ResourceServiceKey controller.ResourceServiceKeyRepository
	ResCatalogAPI      catalog.ResourceCatalogRepository
	ResGroupAPI        management.ResourceGroupRepository
}

type API struct {
	client   *Client
	s3client *S3Client
	opts     *Options
}

// New creates an IBMCloud API wrapper
func New(opts *Options) (*API, error) {
	c := &Client{}

	bluemixSess, err := bluemixsession.New(&bluemix.Config{
		BluemixAPIKey:         opts.ApiKey,
		TokenProviderEndpoint: &tokenProviderEndpoint,
		Debug:                 false,
	})
	if err != nil {
		return nil, err
	}

	c.Session = bluemixSess

	// pre-flight check to authenticate user
	iamAuthRepo, err := authentication.NewIAMAuthRepository(bluemixSess.Config, &rest.Client{
		DefaultHeader: gohttp.Header{
			"User-Agent": []string{http.UserAgent()},
		},
	})
	if err != nil {
		return nil, err
	}

	err = iamAuthRepo.AuthenticateAPIKey(bluemixSess.Config.BluemixAPIKey)
	if err != nil {
		return nil, err
	}

	ctrlv2, err := controllerv2.New(bluemixSess)
	if err != nil {
		return nil, err
	}

	ctrlv1, err := controller.New(bluemixSess)
	if err != nil {
		return nil, err
	}

	catalogClient, err := catalog.New(bluemixSess)
	if err != nil {
		return nil, err
	}

	managementClient, err := management.New(bluemixSess)
	if err != nil {
		return nil, err
	}

	c.ResourceClientV2 = ctrlv2.ResourceServiceInstanceV2()
	c.ResourceClientV1 = ctrlv1.ResourceServiceInstance()
	c.ResourceServiceKey = ctrlv1.ResourceServiceKey()
	c.ResCatalogAPI = catalogClient.ResourceCatalog()
	c.ResGroupAPI = managementClient.ResourceGroup()

	api := &API{
		client: c,
		opts:   opts,
	}

	return api, nil
}

// ListCloudObjectStorageInstances list all available cloud object storage instances of particular servicetype
func (a *API) ListCloudObjectStorageInstances() (map[string]string, error) {
	svcs, err := a.client.ResourceClientV2.ListInstances(controllerv2.ServiceInstanceQuery{
		Type: "service_instance",
	})

	if err != nil {
		return nil, err
	}

	instances := make(map[string]string)

	for _, svc := range svcs {
		if svc.Crn.ServiceName == defaultCloudObjectStorageServiceType {
			instances[svc.Name] = svc.Guid
		}
	}
	return instances, nil
}

// CreateCloudObjectStorageInstance creates a cloud object storage instance
func (a *API) CreateCloudObjectStorageInstance(storageName string, resourceGroup string) (string, error) {
	//Check Service using service type and returns []models.Service
	service, err := a.client.ResCatalogAPI.FindByName(defaultCloudObjectStorageServiceType, true)
	if err != nil {
		return "", err
	}

	//GetServicePlanID takes models.Service as the input and returns serviceplanid as the output
	servicePlanID, err := a.client.ResCatalogAPI.GetServicePlanID(service[0], defaultCloudObjectStorageServicePlan)
	if err != nil {
		return "", err
	}

	if servicePlanID == "" {
		_, err := a.client.ResCatalogAPI.GetServicePlan(defaultCloudObjectStorageServicePlan)
		if err != nil {
			return "", err
		}
		servicePlanID = defaultCloudObjectStorageServicePlan
	}

	deployments, err := a.client.ResCatalogAPI.ListDeployments(servicePlanID)
	if err != nil {
		return "", err
	}

	if len(deployments) == 0 {
		return "", fmt.Errorf("no deployment found for service plan : %s", defaultCloudObjectStorageServicePlan)
	}

	supportedDeployments := []models.ServiceDeployment{}
	supportedLocations := make(map[string]bool)
	for _, d := range deployments {
		if d.Metadata.RCCompatible {
			deploymentLocation := d.Metadata.Deployment.Location
			supportedLocations[deploymentLocation] = true
			if deploymentLocation == defaultResourceGroupAPIRegion {
				supportedDeployments = append(supportedDeployments, d)
			}
		}
	}

	if len(supportedDeployments) == 0 {
		locationList := make([]string, 0, len(supportedLocations))
		for l := range supportedLocations {
			locationList = append(locationList, l)
		}
		return "", fmt.Errorf("no deployment found for service plan %s at location %s\nvalid location(s) are: %q\nuse service instance example if the service is a Cloud Foundry service",
			defaultCloudObjectStorageServicePlan, defaultResourceGroupAPIRegion, locationList)
	}

	//FindByName returns []models.ResourceGroup
	resGrp, err := a.client.ResGroupAPI.FindByName(nil, resourceGroup)
	if err != nil {
		return "", err
	}

	plog.Infof("Creating Cloud Object Storage instance %q Resource Group: %q Deployment: %q ...", storageName, resGrp[0].Name,
		supportedDeployments[0].CatalogCRN)

	var serviceInstancePayload = controller.CreateServiceInstanceRequest{
		Name:            storageName,
		ServicePlanID:   servicePlanID,
		ResourceGroupID: resGrp[0].ID,
		TargetCrn:       supportedDeployments[0].CatalogCRN,
	}

	serviceInstance, err := a.client.ResourceClientV1.CreateInstance(serviceInstancePayload)
	if err != nil {
		return "", err
	}

	plog.Infof("Cloud Object Storage Instance created: %v, InstanceID: %v\n", serviceInstance.Name, serviceInstance.Crn.ServiceInstance)
	return serviceInstance.Crn.ServiceInstance, nil
}

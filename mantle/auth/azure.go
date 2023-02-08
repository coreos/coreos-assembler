// Copyright 2023 Red Hat
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

package auth

import (
	"encoding/json"
	"io"
	"os"
	"os/user"
	"path/filepath"
)

const (
	AzureCredentialsPath = ".azure/azureCreds.json"
)

type AzureCredentials struct {
	ClientID       string `json:"appId"`
	ClientSecret   string `json:"password"`
	SubscriptionID string `json:"subscription"`
	TenantID       string `json:"tenant"`
}

// ReadAzureCredentials picks up the credentials as described in the docs.
//
// If path is empty, $AZURE_CREDENTIALS or $HOME/.azure/azureCreds.json is read.
func ReadAzureCredentials(path string) (AzureCredentials, error) {
	var azCreds AzureCredentials
	if path == "" {
		path = os.Getenv("AZURE_CREDENTIALS")
		if path == "" {
			user, err := user.Current()
			if err != nil {
				return azCreds, err
			}
			path = filepath.Join(user.HomeDir, AzureCredentialsPath)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return azCreds, err
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return azCreds, err
	}

	err = json.Unmarshal(content, &azCreds)
	if err != nil {
		return azCreds, err
	}

	return azCreds, nil
}

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

package main

import (
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/spf13/pflag"

	"github.com/coreos/mantle/lang/maps"
	"github.com/coreos/mantle/sdk"
)

var (
	specBoard         string
	specChannel       string
	specVersion       string
	gceBoards         = []string{"amd64-usr"}
	azureBoards       = []string{"amd64-usr"}
	awsBoards         = []string{"amd64-usr"}
	azureEnvironments = []azureEnvironmentSpec{
		azureEnvironmentSpec{
			SubscriptionName:     "BizSpark",
			AdditionalContainers: []string{"pre-publish"},
		},
		azureEnvironmentSpec{
			SubscriptionName: "BlackForest",
		},
		azureEnvironmentSpec{
			SubscriptionName: "Mooncake",
		},
	}
	awsPartitions = []awsPartitionSpec{
		awsPartitionSpec{
			Name:         "AWS",
			Profile:      "default",
			Bucket:       "coreos-prod-ami-import-us-west-2",
			BucketRegion: "us-west-2",
			LaunchPermissions: []string{
				"017021077683", // coreos-cl
			},
			Regions: []string{
				"us-east-1",
				"us-east-2",
				"us-west-1",
				"us-west-2",
				"eu-west-1",
				"eu-west-2",
				"eu-west-3",
				"eu-central-1",
				"eu-north-1",
				"ap-south-1",
				"ap-southeast-1",
				"ap-southeast-2",
				"ap-northeast-1",
				"ap-northeast-2",
				"sa-east-1",
				"ca-central-1",
			},
		},
		awsPartitionSpec{
			Name:         "AWS GovCloud",
			Profile:      "govcloud",
			Bucket:       "coreos-prod-ami-import-us-gov-west-1",
			BucketRegion: "us-gov-west-1",
			Regions: []string{
				"us-gov-west-1",
				"us-gov-east-1",
			},
		},
		awsPartitionSpec{
			Name:         "AWS China",
			Profile:      "china",
			Bucket:       "coreos-prod-ami-import-cn-north-1",
			BucketRegion: "cn-north-1",
			Regions: []string{
				"cn-north-1",
				"cn-northwest-1",
			},
		},
	}
	specs = map[string]channelSpec{
		"user": channelSpec{
			BaseURL: "gs://users.developer.core-os.net/" + os.Getenv("USER") + "/boards",
			Boards:  []string{"amd64-usr"},
			Destinations: []storageSpec{storageSpec{
				BaseURL:       "gs://users.developer.core-os.net/" + os.Getenv("USER") + "/releases",
				NamedPath:     "current",
				VersionPath:   true,
				DirectoryHTML: true,
				IndexHTML:     true,
			}},
			GCE: gceSpec{
				Project:     "coreos-gce-testing",
				Family:      "coreos-user",
				Description: "CoreOS Container Linux development image",
				Image:       "coreos_production_gce.tar.gz",
				Publish:     "coreos_production_gce.txt",
				Limit:       2,
			},
			AWS: awsSpec{
				BaseName:        "Container-Linux",
				BaseDescription: "CoreOS Container Linux development image",
				Prefix:          "coreos_production_ami_",
				Image:           "coreos_production_ami_vmdk_image.vmdk.bz2",
				Partitions: []awsPartitionSpec{
					awsPartitionSpec{
						Name:         "AWS West",
						Profile:      "coreos-cl",
						Bucket:       "coreos-dev-ami-import-us-west-2",
						BucketRegion: "us-west-2",
						Regions: []string{
							"us-west-1",
							"us-west-2",
							"eu-west-3", // no PV
						},
					},
					awsPartitionSpec{
						// partition with a single region
						Name:         "AWS East",
						Profile:      "coreos-cl",
						Bucket:       "coreos-dev-ami-import-us-east-1",
						BucketRegion: "us-east-1",
						LaunchPermissions: []string{
							"595879546273", // prod account, to test LaunchPermissions
						},
						Regions: []string{
							"us-east-1",
						},
					},
				},
			},
		},
		"developer": channelSpec{
			BaseURL: "gs://builds.developer.core-os.net/boards",
			Boards:  []string{"amd64-usr"},
			GCE: gceSpec{
				Project:     "coreos-gce-testing",
				Family:      "coreos-developer",
				Description: "CoreOS Container Linux development image",
				Image:       "coreos_production_gce.tar.gz",
				Publish:     "coreos_production_gce.txt",
				Limit:       25,
			},
			AWS: awsSpec{
				BaseName:        "Container-Linux",
				BaseDescription: "CoreOS Container Linux development image",
				Prefix:          "coreos_production_ami_",
				Image:           "coreos_production_ami_vmdk_image.vmdk.bz2",
				Partitions: []awsPartitionSpec{
					awsPartitionSpec{
						Name:         "AWS West",
						Profile:      "coreos-cl",
						Bucket:       "coreos-dev-ami-import-us-west-2",
						BucketRegion: "us-west-2",
						Regions: []string{
							"us-west-2",
						},
					},
				},
			},
		},
		"alpha": channelSpec{
			BaseURL: "gs://builds.release.core-os.net/alpha/boards",
			Boards:  []string{"amd64-usr"},
			Destinations: []storageSpec{storageSpec{
				BaseURL:     "gs://alpha.release.core-os.net",
				NamedPath:   "current",
				VersionPath: true,
				IndexHTML:   true,
			}, storageSpec{
				BaseURL:       "gs://coreos-alpha",
				Title:         "alpha.release.core-os.net",
				NamedPath:     "current",
				VersionPath:   true,
				DirectoryHTML: true,
				IndexHTML:     true,
			}, storageSpec{
				BaseURL:     "gs://storage.core-os.net/coreos",
				NamedPath:   "alpha",
				VersionPath: true,
				IndexHTML:   true,
			}, storageSpec{
				BaseURL:       "gs://coreos-net-storage/coreos",
				Title:         "storage.core-os.net",
				NamedPath:     "alpha",
				VersionPath:   true,
				DirectoryHTML: true,
				IndexHTML:     true,
			}},
			GCE: gceSpec{
				Project:     "coreos-cloud",
				Family:      "coreos-alpha",
				Description: "CoreOS, CoreOS alpha",
				Licenses:    []string{"coreos-alpha"},
				Image:       "coreos_production_gce.tar.gz",
				Publish:     "coreos_production_gce.txt",
				Limit:       25,
			},
			Azure: azureSpec{
				Offer:             "CoreOS",
				Image:             "coreos_production_azure_image.vhd.bz2",
				StorageAccount:    "coreos",
				Container:         "publish",
				Environments:      azureEnvironments,
				Label:             "CoreOS Alpha",
				Description:       "The Alpha channel closely tracks current development work and is released frequently. The newest versions of the Linux kernel, systemd, and other components will be available for testing.",
				RecommendedVMSize: "Medium",
				IconURI:           "coreos-globe-color-lg-100px.png",
				SmallIconURI:      "coreos-globe-color-lg-45px.png",
			},
			AWS: awsSpec{
				BaseName:        "CoreOS",
				BaseDescription: "CoreOS Container Linux",
				Prefix:          "coreos_production_ami_",
				Image:           "coreos_production_ami_vmdk_image.vmdk.bz2",
				Partitions:      awsPartitions,
			},
		},
		"beta": channelSpec{
			BaseURL: "gs://builds.release.core-os.net/beta/boards",
			Boards:  []string{"amd64-usr"},
			Destinations: []storageSpec{storageSpec{
				BaseURL:     "gs://beta.release.core-os.net",
				NamedPath:   "current",
				VersionPath: true,
				IndexHTML:   true,
			}, storageSpec{
				BaseURL:       "gs://coreos-beta",
				Title:         "beta.release.core-os.net",
				NamedPath:     "current",
				VersionPath:   true,
				DirectoryHTML: true,
				IndexHTML:     true,
			}, storageSpec{
				BaseURL:   "gs://storage.core-os.net/coreos",
				NamedPath: "beta",
				IndexHTML: true,
			}, storageSpec{
				BaseURL:       "gs://coreos-net-storage/coreos",
				Title:         "storage.core-os.net",
				NamedPath:     "beta",
				DirectoryHTML: true,
				IndexHTML:     true,
			}},
			GCE: gceSpec{
				Project:     "coreos-cloud",
				Family:      "coreos-beta",
				Description: "CoreOS, CoreOS beta",
				Licenses:    []string{"coreos-beta"},
				Image:       "coreos_production_gce.tar.gz",
				Publish:     "coreos_production_gce.txt",
				Limit:       25,
			},
			Azure: azureSpec{
				Offer:             "CoreOS",
				Image:             "coreos_production_azure_image.vhd.bz2",
				StorageAccount:    "coreos",
				Container:         "publish",
				Environments:      azureEnvironments,
				Label:             "CoreOS Beta",
				Description:       "The Beta channel consists of promoted Alpha releases. Mix a few Beta machines into your production clusters to catch any bugs specific to your hardware or configuration.",
				RecommendedVMSize: "Medium",
				IconURI:           "coreos-globe-color-lg-100px.png",
				SmallIconURI:      "coreos-globe-color-lg-45px.png",
			},
			AWS: awsSpec{
				BaseName:        "CoreOS",
				BaseDescription: "CoreOS Container Linux",
				Prefix:          "coreos_production_ami_",
				Image:           "coreos_production_ami_vmdk_image.vmdk.bz2",
				Partitions:      awsPartitions,
			},
		},
		"stable": channelSpec{
			BaseURL: "gs://builds.release.core-os.net/stable/boards",
			Boards:  []string{"amd64-usr"},
			Destinations: []storageSpec{storageSpec{
				BaseURL:     "gs://stable.release.core-os.net",
				NamedPath:   "current",
				VersionPath: true,
				IndexHTML:   true,
			}, storageSpec{
				BaseURL:       "gs://coreos-stable",
				Title:         "stable.release.core-os.net",
				NamedPath:     "current",
				VersionPath:   true,
				DirectoryHTML: true,
				IndexHTML:     true,
			}},
			GCE: gceSpec{
				Project:     "coreos-cloud",
				Family:      "coreos-stable",
				Description: "CoreOS, CoreOS stable",
				Licenses:    []string{"coreos-stable"},
				Image:       "coreos_production_gce.tar.gz",
				Publish:     "coreos_production_gce.txt",
				Limit:       25,
			},
			Azure: azureSpec{
				Offer:             "CoreOS",
				Image:             "coreos_production_azure_image.vhd.bz2",
				StorageAccount:    "coreos",
				Container:         "publish",
				Environments:      azureEnvironments,
				Label:             "CoreOS Stable",
				Description:       "The Stable channel should be used by production clusters. Versions of CoreOS are battle-tested within the Beta and Alpha channels before being promoted.",
				RecommendedVMSize: "Medium",
				IconURI:           "coreos-globe-color-lg-100px.png",
				SmallIconURI:      "coreos-globe-color-lg-45px.png",
			},
			AWS: awsSpec{
				BaseName:        "CoreOS",
				BaseDescription: "CoreOS Container Linux",
				Prefix:          "coreos_production_ami_",
				Image:           "coreos_production_ami_vmdk_image.vmdk.bz2",
				Partitions:      awsPartitions,
			},
		},
	}
)

func AddSpecFlags(flags *pflag.FlagSet) {
	board := sdk.DefaultBoard()
	channels := strings.Join(maps.SortedKeys(specs), " ")
	versions, _ := sdk.VersionsFromManifest()
	flags.StringVarP(&specBoard, "board", "B",
		board, "target board")
	flags.StringVarP(&specChannel, "channel", "C",
		"user", "channels: "+channels)
	flags.StringVarP(&specVersion, "version", "V",
		versions.VersionID, "release version")
}

func ChannelSpec() channelSpec {
	if specBoard == "" {
		plog.Fatal("--board is required")
	}
	if specChannel == "" {
		plog.Fatal("--channel is required")
	}
	if specVersion == "" {
		plog.Fatal("--version is required")
	}

	spec, ok := specs[specChannel]
	if !ok {
		plog.Fatalf("Unknown channel: %s", specChannel)
	}

	boardOk := false
	for _, board := range spec.Boards {
		if specBoard == board {
			boardOk = true
			break
		}
	}
	if !boardOk {
		plog.Fatalf("Unknown board %q for channel %q", specBoard, specChannel)
	}

	gceOk := false
	for _, board := range gceBoards {
		if specBoard == board {
			gceOk = true
			break
		}
	}
	if !gceOk {
		spec.GCE = gceSpec{}
	}

	azureOk := false
	for _, board := range azureBoards {
		if specBoard == board {
			azureOk = true
			break
		}
	}
	if !azureOk {
		spec.Azure = azureSpec{}
	}

	awsOk := false
	for _, board := range awsBoards {
		if specBoard == board {
			awsOk = true
			break
		}
	}
	if !awsOk {
		spec.AWS = awsSpec{}
	}

	return spec
}

func (cs channelSpec) SourceURL() string {
	u, err := url.Parse(cs.BaseURL)
	if err != nil {
		panic(err)
	}
	u.Path = path.Join(u.Path, specBoard, specVersion)
	return u.String()
}

func (ss storageSpec) ParentPrefixes() []string {
	u, err := url.Parse(ss.BaseURL)
	if err != nil {
		panic(err)
	}
	return []string{u.Path, path.Join(u.Path, specBoard)}
}

func (ss storageSpec) FinalPrefixes() []string {
	u, err := url.Parse(ss.BaseURL)
	if err != nil {
		plog.Panic(err)
	}

	prefixes := []string{}
	if ss.VersionPath {
		prefixes = append(prefixes,
			path.Join(u.Path, specBoard, specVersion))
	}
	if ss.NamedPath != "" {
		prefixes = append(prefixes,
			path.Join(u.Path, specBoard, ss.NamedPath))
	}
	if len(prefixes) == 0 {
		plog.Panicf("Invalid destination: %#v", ss)
	}

	return prefixes
}

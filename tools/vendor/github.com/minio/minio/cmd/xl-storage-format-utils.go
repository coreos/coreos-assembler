/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	jsoniter "github.com/json-iterator/go"
)

type versionsSorter []FileInfo

func (v versionsSorter) Len() int      { return len(v) }
func (v versionsSorter) Swap(i, j int) { v[i], v[j] = v[j], v[i] }
func (v versionsSorter) Less(i, j int) bool {
	if v[i].IsLatest {
		return true
	}
	if v[j].IsLatest {
		return false
	}
	return v[i].ModTime.After(v[j].ModTime)
}

func getFileInfoVersions(xlMetaBuf []byte, volume, path string) (FileInfoVersions, error) {
	if isXL2V1Format(xlMetaBuf) {
		var xlMeta xlMetaV2
		if err := xlMeta.Load(xlMetaBuf); err != nil {
			return FileInfoVersions{}, err
		}
		versions, latestModTime, err := xlMeta.ListVersions(volume, path)
		if err != nil {
			return FileInfoVersions{}, err
		}
		return FileInfoVersions{
			Volume:        volume,
			Name:          path,
			Versions:      versions,
			LatestModTime: latestModTime,
		}, nil
	}

	xlMeta := &xlMetaV1Object{}
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	if err := json.Unmarshal(xlMetaBuf, xlMeta); err != nil {
		return FileInfoVersions{}, errFileCorrupt
	}

	fi, err := xlMeta.ToFileInfo(volume, path)
	if err != nil {
		return FileInfoVersions{}, err
	}

	fi.IsLatest = true // No versions so current version is latest.
	fi.XLV1 = true     // indicates older version
	return FileInfoVersions{
		Volume:        volume,
		Name:          path,
		Versions:      []FileInfo{fi},
		LatestModTime: fi.ModTime,
	}, nil
}

func getFileInfo(xlMetaBuf []byte, volume, path, versionID string) (FileInfo, error) {
	if isXL2V1Format(xlMetaBuf) {
		var xlMeta xlMetaV2
		if err := xlMeta.Load(xlMetaBuf); err != nil {
			return FileInfo{}, err
		}
		return xlMeta.ToFileInfo(volume, path, versionID)
	}

	xlMeta := &xlMetaV1Object{}
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	if err := json.Unmarshal(xlMetaBuf, xlMeta); err != nil {
		return FileInfo{}, errFileCorrupt
	}
	fi, err := xlMeta.ToFileInfo(volume, path)
	if err == errFileNotFound && versionID != "" {
		return fi, errFileVersionNotFound
	}
	fi.XLV1 = true // indicates older version
	return fi, err
}

/*
 * zipindex, (C)2021 MinIO, Inc.
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

// Package zipindex provides a size optimized representation of a zip file to allow
// decompressing the file without reading the zip file index.
//
// It will only provide the minimal needed data for successful decompression and CRC checks.
//
// Custom metadata can be stored per file and filtering can be performed on the incoming files.
package zipindex

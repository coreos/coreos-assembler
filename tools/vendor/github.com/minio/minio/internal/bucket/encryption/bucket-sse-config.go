// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"

	"github.com/minio/minio/internal/crypto"
	xhttp "github.com/minio/minio/internal/http"
)

const (
	// AES256 is used with SSE-S3
	AES256 SSEAlgorithm = "AES256"
	// AWSKms is used with SSE-KMS
	AWSKms SSEAlgorithm = "aws:kms"
)

// SSEAlgorithm - represents valid SSE algorithms supported; currently only AES256 is supported
type SSEAlgorithm string

// UnmarshalXML - Unmarshals XML tag to valid SSE algorithm
func (alg *SSEAlgorithm) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return err
	}

	switch s {
	case string(AES256):
		*alg = AES256
	case string(AWSKms):
		*alg = AWSKms
	default:
		return errors.New("Unknown SSE algorithm")
	}

	return nil
}

// MarshalXML - Marshals given SSE algorithm to valid XML
func (alg *SSEAlgorithm) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return e.EncodeElement(string(*alg), start)
}

// EncryptionAction - for ApplyServerSideEncryptionByDefault XML tag
type EncryptionAction struct {
	Algorithm   SSEAlgorithm `xml:"SSEAlgorithm,omitempty"`
	MasterKeyID string       `xml:"KMSMasterKeyID,omitempty"`
}

// SSERule - for ServerSideEncryptionConfiguration XML tag
type SSERule struct {
	DefaultEncryptionAction EncryptionAction `xml:"ApplyServerSideEncryptionByDefault"`
}

const xmlNS = "http://s3.amazonaws.com/doc/2006-03-01/"

// BucketSSEConfig - represents default bucket encryption configuration
type BucketSSEConfig struct {
	XMLNS   string    `xml:"xmlns,attr,omitempty"`
	XMLName xml.Name  `xml:"ServerSideEncryptionConfiguration"`
	Rules   []SSERule `xml:"Rule"`
}

// ParseBucketSSEConfig - Decodes given XML to a valid default bucket encryption config
func ParseBucketSSEConfig(r io.Reader) (*BucketSSEConfig, error) {
	var config BucketSSEConfig
	err := xml.NewDecoder(r).Decode(&config)
	if err != nil {
		return nil, err
	}

	// Validates server-side encryption config rules
	// Only one rule is allowed on AWS S3
	if len(config.Rules) != 1 {
		return nil, errors.New("only one server-side encryption rule is allowed at a time")
	}

	for _, rule := range config.Rules {
		switch rule.DefaultEncryptionAction.Algorithm {
		case AES256:
			if rule.DefaultEncryptionAction.MasterKeyID != "" {
				return nil, errors.New("MasterKeyID is allowed with aws:kms only")
			}
		case AWSKms:
			if rule.DefaultEncryptionAction.MasterKeyID == "" {
				return nil, errors.New("MasterKeyID is missing with aws:kms")
			}
		}
	}

	if config.XMLNS == "" {
		config.XMLNS = xmlNS
	}
	return &config, nil
}

// Apply applies the SSE bucket configuration on the given HTTP headers and
// sets the specified SSE headers.
//
// Apply does not overwrite any existing SSE headers. Further, it will
// set minimal SSE-KMS headers if autoEncrypt is true and the BucketSSEConfig
// is nil.
func (b *BucketSSEConfig) Apply(headers http.Header, autoEncrypt bool) {
	if _, ok := crypto.IsRequested(headers); ok {
		return
	}
	if b == nil {
		if autoEncrypt {
			headers.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionKMS)
		}
		return
	}

	switch b.Algorithm() {
	case xhttp.AmzEncryptionAES:
		headers.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionAES)
	case xhttp.AmzEncryptionKMS:
		headers.Set(xhttp.AmzServerSideEncryption, xhttp.AmzEncryptionKMS)
		headers.Set(xhttp.AmzServerSideEncryptionKmsID, b.KeyID())
	}
}

// Algorithm returns the SSE algorithm specified by the SSE configuration.
func (b *BucketSSEConfig) Algorithm() SSEAlgorithm {
	for _, rule := range b.Rules {
		return rule.DefaultEncryptionAction.Algorithm
	}
	return ""
}

// KeyID returns the KMS key ID specified by the SSE configuration.
// If the SSE configuration does not specify SSE-KMS it returns an
// empty key ID.
func (b *BucketSSEConfig) KeyID() string {
	for _, rule := range b.Rules {
		return rule.DefaultEncryptionAction.MasterKeyID
	}
	return ""
}

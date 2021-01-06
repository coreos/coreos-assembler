package stream

// Metadata for a release or stream
type Metadata struct {
	LastModified string `json:"last-modified"`
}

// ImageFormat contains all artifacts for a single OS image
type ImageFormat struct {
	Disk      *Artifact `json:"disk,omitempty"`
	Kernel    *Artifact `json:"kernel,omitempty"`
	Initramfs *Artifact `json:"initramfs,omitempty"`
	Rootfs    *Artifact `json:"rootfs,omitempty"`
}

// Artifact represents one image file, plus its metadata
type Artifact struct {
	Location  string `json:"location"`
	Signature string `json:"signature"`
	Sha256    string `json:"sha256"`
}

// GcpImage represents a GCP cloud image
type GcpImage struct {
	Project string `json:"project,omitempty"`
	Family  string `json:"family,omitempty"`
	Name    string `json:"name,omitempty"`
}

// Stream contains artifacts available in a stream
type Stream struct {
	Stream        string          `json:"stream"`
	Metadata      Metadata        `json:"metadata"`
	Architectures map[string]Arch `json:"architectures"`
}

// Architecture release details
type Arch struct {
	Artifacts map[string]PlatformArtifacts `json:"artifacts"`
	Images    Images                       `json:"images,omitempty"`
}

// PlatformArtifacts contains images for a platform
type PlatformArtifacts struct {
	Release string                 `json:"release"`
	Formats map[string]ImageFormat `json:"formats"`
}

// Images contains images available in cloud providers
type Images struct {
	Aws *AwsImage `json:"aws,omitempty"`
	Gcp *GcpImage `json:"gcp,omitempty"`
}

// AwsImage represents an image across all AWS regions
type AwsImage struct {
	Regions map[string]AwsRegionImage `json:"regions,omitempty"`
}

// AwsRegionImage represents an image in one AWS region
type AwsRegionImage struct {
	Release string `json:"release"`
	Image   string `json:"image"`
}

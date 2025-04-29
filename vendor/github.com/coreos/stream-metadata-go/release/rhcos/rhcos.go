package rhcos

// Extensions is data specific to Red Hat Enterprise Linux CoreOS
type Extensions struct {
	AwsWinLi  *AwsWinLi  `json:"aws-winli,omitempty"`
	AzureDisk *AzureDisk `json:"azure-disk,omitempty"`
}

// AzureDisk represents an Azure cloud image.
type AzureDisk struct {
	// URL to an image already stored in Azure infrastructure
	// that can be copied into an image gallery.  Avoid creating VMs directly
	// from this URL as that may lead to performance limitations.
	URL string `json:"url,omitempty"`
}

// AwsWinLi represents prebuilt AWS Windows License Included Images.
type AwsWinLi struct {
	// A mapping of AWS region names (e.g. "us-east-1") to CloudImage
	// descriptors. Each entry provides metadata for the corresponding
	// AWS Windows LI AMI.
	Images map[string]CloudImage `json:"images"`
}

// CloudImage generic image detail
// This struct was copied from the release package to avoid an import cycle,
// and is used to describe individual AWS WinLI Images.
type CloudImage struct {
	Image string `json:"image"`
}

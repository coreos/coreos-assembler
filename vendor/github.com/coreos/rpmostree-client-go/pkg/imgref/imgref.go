// package imgref parses ostree-container image references.
package imgref

import (
	"fmt"
	"strings"
)

// SignatureVerify is a mirror of https://docs.rs/ostree-ext/latest/ostree_ext/container/enum.SignatureSource.html
type SignatureVerify struct {
	AllowInsecure bool
	OstreeRemote  string
}

type ImageReferenceWithTransport struct {
	// IsRegistry is true if this image is fetched from a registry
	Transport string
	// Image is the unparsed string representation of a container image.
	// For e.g. oci-archive: it will be a filesystem path.
	// It can include a tag or digest (or not).
	Image string
}

// OstreeImage reference captures an ostree signature verification policy alongside an image reference.
// This mirrors https://docs.rs/ostree-ext/latest/ostree_ext/container/struct.OstreeImageReference.html
type OstreeImageReference struct {
	Sigverify SignatureVerify
	Imgref    ImageReferenceWithTransport
}

// IsRegistry returns true if this image will be fetched from a registry.
func (ir *ImageReferenceWithTransport) IsRegistry() bool {
	return ir.Transport == "registry"
}

// parseImageReference mirrors https://docs.rs/ostree-ext/0.12.0/src/ostree_ext/container/mod.rs.html#129
func parseImageReference(ir string) (*ImageReferenceWithTransport, error) {
	irparts := strings.SplitN(ir, ":", 2)
	if len(irparts) < 2 {
		return nil, fmt.Errorf("invalid image reference (missing ':'): %s", ir)
	}

	imgref := ImageReferenceWithTransport{
		Transport: irparts[0],
		Image:     irparts[1],
	}
	// docker:// is a special case; we want to rename it and also trim the //
	if imgref.Transport == "docker" {
		imgref.Transport = "registry"
		if !strings.HasPrefix(imgref.Image, "//") {
			return nil, fmt.Errorf("missing // in docker://")
		}
		imgref.Image = imgref.Image[2:]
	}
	return &imgref, nil
}

func Parse(ir string) (*OstreeImageReference, error) {
	parts := strings.SplitN(ir, ":", 2)
	if len(parts) != 2 {
		panic("Expected 2 parts")
	}
	first := parts[0]
	second := parts[1]
	sigverify := SignatureVerify{}
	rest := second
	switch first {
	case "ostree-image-signed":
	case "ostree-unverified-image":
		sigverify = SignatureVerify{AllowInsecure: true}
	case "ostree-unverified-registry":
		sigverify = SignatureVerify{AllowInsecure: true}
		rest = "registry:" + second
	case "ostree-remote-registry":
		subparts := strings.SplitN(rest, ":", 2)
		sigverify = SignatureVerify{OstreeRemote: subparts[0], AllowInsecure: true}
		rest = "registry:" + subparts[1]
	case "ostree-remote-image":
		subparts := strings.SplitN(rest, ":", 2)
		sigverify = SignatureVerify{OstreeRemote: subparts[0], AllowInsecure: true}
		rest = subparts[1]
	default:
		return nil, fmt.Errorf("invalid ostree image reference (unmatched scheme): %s", ir)
	}

	imgref, err := parseImageReference(rest)
	if err != nil {
		return nil, err
	}
	return &OstreeImageReference{Sigverify: sigverify, Imgref: *imgref}, nil
}

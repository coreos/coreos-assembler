package auth

// oauth2 imports google.golang.org/cloud/compute/metadata but godep insists
// on deleting it without something importing google.golang.org/cloud too.
import (
	_ "google.golang.org/cloud"
)

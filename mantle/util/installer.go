package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Note this is a no-op if the decompressed dest already exists.
func DownloadImageAndDecompress(url, compressedDest string, skipSignature bool) (string, error) {
	var decompressedDest = strings.TrimSuffix(strings.TrimSuffix(compressedDest, ".xz"), ".gz")
	if exists, err := PathExists(decompressedDest); err != nil {
		return "", err
	} else if !exists {
		targetdir := filepath.Dir(decompressedDest)
		downloadArgs := []string{"download", "--decompress", "-C", targetdir}
		if skipSignature {
			downloadArgs = append(downloadArgs, "--insecure")
		}
		downloadArgs = append(downloadArgs, []string{"-u", url}...)
		cmd := exec.Command("coreos-installer", downloadArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", err
		}
	}
	return decompressedDest, nil
}

package remote

import (
	"archive/tar"
	"compress/gzip"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
)

// CosaArchive describes the directory names to create empty directories in the tar ball
// and the directories to include when creating the tar ball
type CosaArchive struct {
	CreateDirs []string //create empty dirs when creating tar ball
	Includes   []string //create bar ball with the dirs
}

// CreateArchive creates tar ball
func (a *CosaArchive) CreateArchive(dest string) error {
	// delete the tar ball file dest if it already exists
	_, err := os.Stat(dest)
	if os.IsExist(err) {
		log.Tracef("dest %s already exists", dest)
		if err := os.Remove(dest); err != nil {
			return err
		}
		log.Tracef("original dest %s deleted\n", dest)
	}

	// create the tar ball file dest
	tarFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer tarFile.Close()

	gWriter := gzip.NewWriter(tarFile)
	defer gWriter.Close()

	tarFileWriter := tar.NewWriter(gWriter)
	defer tarFileWriter.Close()

	// write directories to the tar ball
	for _, path := range a.Includes {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		err = writeArchive(file, "", tarFileWriter)
		if err != nil {
			return err
		}
		log.Debugf("wrote %s to tarball", path)
	}

	// create empty directories in the tar ball
	for _, dir := range a.CreateDirs {
		err := os.Mkdir(dir, 0755)
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		file, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer file.Close()

		if err := createDirInArchive(file, file.Name(), tarFileWriter); err != nil {
			return err
		}
		log.Debugf("created %v dir in tarball", dir)
	}

	log.Infof("created tar ball: %v", dest)
	return nil
}

// writeArchive writes files and directories recursively to the tar ball
func writeArchive(file *os.File, prefix string, writer *tar.Writer) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}

	if info.IsDir() {
		if prefix == "" {
			prefix = info.Name()
		} else {
			prefix = prefix + "/" + info.Name()
		}

		readdir, err := file.Readdir(-1)
		if err != nil {
			return err
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = prefix

		if err = writer.WriteHeader(hdr); err != nil {
			return err
		}

		for _, fi := range readdir {
			f, err := os.Open(file.Name() + "/" + fi.Name())
			if err != nil {
				return err
			}
			err = writeArchive(f, prefix, writer)
			if err != nil {
				return err
			}
		}
	} else {
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		header.Name = prefix + "/" + header.Name
		if err != nil {
			return err
		}

		err = writer.WriteHeader(header)
		if err != nil {
			return err
		}

		_, err = io.Copy(writer, file)
		if err != nil {
			return err
		}
	}

	return nil
}

// createDirInArchive write directory itself to the tar ball
func createDirInArchive(file *os.File, prefix string, writer *tar.Writer) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = prefix

	if err = writer.WriteHeader(hdr); err != nil {
		return err
	}

	return nil
}

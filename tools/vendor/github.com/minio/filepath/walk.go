// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the https://golang.org/LICENSE file.

// Package filepath is a separate implementation of Go's filepath.Walk()
// to cater for flat key style sorted walk.
package filepath

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// Implementation is much like filepath.Walk() but re-implemented to
// avoid Stat() per file, lexically ordered output treats directories
// with `/` at the end before sorting, this is sorting is flat key
// sorting instead of regular filesystem sorting of entries. Also
// uses ErrSkipFile additional to ErrSkipDir to avoid errors from
// unreadable files on the filesystem.

// Walk walks the file tree rooted at root, calling walkFn for each file or
// directory in the tree, including root.
func Walk(root string, walkFn WalkFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		return walkFn(root, nil, err)
	}
	return walk(root, info, walkFn)
}

// byName implements sort.Interface for sorting os.FileInfo list.
type byName []os.FileInfo

func (f byName) Len() int      { return len(f) }
func (f byName) Swap(i, j int) { f[i], f[j] = f[j], f[i] }
func (f byName) Less(i, j int) bool {
	n1 := f[i].Name()
	if f[i].IsDir() {
		n1 = n1 + string(os.PathSeparator)
	}

	n2 := f[j].Name()
	if f[j].IsDir() {
		n2 = n2 + string(os.PathSeparator)
	}

	return n1 < n2
}

// readDir reads the directory named by dirname and returns
// a sorted list of directory entries.
func readDir(dirname string) (fi []os.FileInfo, err error) {
	f, err := os.Open(dirname)
	if err == nil {
		defer f.Close()
		if fi, err = f.Readdir(-1); fi != nil {
			sort.Sort(byName(fi))
		}
	}

	return
}

// WalkFunc is the type of the function called for each file or directory
// visited by Walk. The path argument contains the argument to Walk as a
// prefix; that is, if Walk is called with "dir", which is a directory
// containing the file "a", the walk function will be called with argument
// "dir/a". The info argument is the os.FileInfo for the named path.
type WalkFunc func(path string, info os.FileInfo, err error) error

// ErrSkipDir is used as a return value from WalkFuncs to indicate that
// the directory named in the call is to be skipped. It is not returned
// as an error by any function.
var ErrSkipDir = errors.New("skip this directory")

// ErrSkipFile is used as a return value from WalkFuncs to indicate that
// the file named in the call is to be skipped. It is not returned
// as an error by any function.
var ErrSkipFile = errors.New("skip this file")

// walk recursively descends path, calling walkFn.
func walk(path string, info os.FileInfo, walkFn WalkFunc) error {
	err := walkFn(path, info, nil)
	if err != nil {
		if info.Mode().IsDir() && err == ErrSkipDir {
			return nil
		}
		if info.Mode().IsRegular() && err == ErrSkipFile {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return nil
	}

	fis, err := readDir(path)
	if err != nil {
		return walkFn(path, info, err)
	}
	for _, fileInfo := range fis {
		filename := filepath.Join(path, fileInfo.Name())
		if err != nil {
			if err = walkFn(filename, fileInfo, err); err != nil && err != ErrSkipDir && err != ErrSkipFile {
				return err
			}
		} else {
			err = walk(filename, fileInfo, walkFn)
			if err != nil {
				if err == ErrSkipDir || err == ErrSkipFile {
					return nil
				}
				return err
			}
		}
	}
	return nil
}

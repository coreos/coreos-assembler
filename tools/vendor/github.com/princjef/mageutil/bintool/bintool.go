package bintool

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cheggaaa/pb/v3"
	"github.com/mattn/go-isatty"
	"github.com/princjef/mageutil/shellcmd"
)

type (
	// BinTool represents a single binary tool/version combination with the
	// information needed to check and/or install the tool if desired.
	//
	// The struct provides a set of utilities for checking the existence of a
	// valid version of the command and installing the command in a way that is
	// scoped to the project at hand, if desired.
	BinTool struct {
		folder     string
		versionCmd string
		url        string
		tmplData   templateData
	}

	templateData struct {
		GOOS       string
		GOARCH     string
		Version    string
		Cmd        string
		FullCmd    string
		ArchiveExt string
		BinExt     string
	}

	// Option configures a BinTool with optional settings
	Option func(t *BinTool) error
)

// Must provides a utility for asserting that methods returning a BinTool and an
// error have no error. If there is an error, this call will panic.
func Must(t *BinTool, err error) *BinTool {
	if err != nil {
		panic(err)
	}

	return t
}

// New initializes a BinTool with the provided command, version, and download
// url. Additional options may be provided to configure things such as the
// version test command, file extensions and folder containing the binary tool.
//
// The command, url, and version command may all use text templates to define
// their formats. If any of these templates fails to compile or evaluate, this
// call will return an error.
func New(command, version, url string, opts ...Option) (*BinTool, error) {
	t := &BinTool{
		folder:     "./bin",
		versionCmd: "{{.FullCmd}} --version",
		tmplData: templateData{
			GOOS:       runtime.GOOS,
			GOARCH:     runtime.GOARCH,
			Version:    version,
			ArchiveExt: ".tar.gz",
			BinExt:     "",
		},
	}

	if runtime.GOOS == "windows" {
		t.tmplData.ArchiveExt = ".zip"
		t.tmplData.BinExt = ".exe"
	}

	for _, opt := range opts {
		if err := opt(t); err != nil {
			return nil, err
		}
	}

	t.folder = filepath.FromSlash(t.folder)

	var err error
	t.tmplData.Cmd, err = resolveTemplate(command, t.tmplData)
	if err != nil {
		return nil, err
	}

	t.tmplData.FullCmd = filepath.Join(t.folder, t.tmplData.Cmd)

	t.versionCmd, err = resolveTemplate(t.versionCmd, t.tmplData)
	if err != nil {
		return nil, err
	}

	t.url, err = resolveTemplate(url, t.tmplData)
	if err != nil {
		return nil, err
	}

	return t, nil
}

// IsInstalled checks whether the correct version of the tool is currently
// installed as defined by the version command.
func (t *BinTool) IsInstalled() bool {
	args := strings.Split(t.versionCmd, " ")
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return false
	}

	byteVersion := []byte(t.tmplData.Version)
	i := bytes.Index(out, byteVersion)

	switch {
	case i == -1:
		return false
	case i == 0 && len(byteVersion) == len(out):
		return true
	case i == 0 && !isAlphanumeric(out[len(byteVersion)]):
		return true
	case i+len(byteVersion) == len(out) && !isAlphanumeric(out[i-1]):
		return true
	case !isAlphanumeric(out[i-1]) && !isAlphanumeric(out[i+len(byteVersion)]):
		return true
	default:
		return false
	}
}

// Install unconditionally downloads and installs the tool to the configured
// folder.
//
// If you don't want to download the tool every time, you may prefer Ensure()
// instead.
func (t *BinTool) Install() error {
	data, err := downloadAndExtract(t.tmplData.Cmd, t.url)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(t.folder, 0755); err != nil {
		return fmt.Errorf("bintool: unable to create destination folder %s: %w", t.folder, err)
	}

	p := filepath.Join(t.folder, t.tmplData.Cmd)
	if err := ioutil.WriteFile(p, data, 0755); err != nil {
		return fmt.Errorf("bintool: unable to write executable file %s: %w", p, err)
	}

	return nil
}

// Ensure checks to see if a valid version of the tool is installed, and
// downloads/installs it if it isn't already.
func (t *BinTool) Ensure() error {
	if t.IsInstalled() {
		return nil
	}

	return t.Install()
}

// Command generates a runnable command using this binary tool along with the
// provided args.
func (t *BinTool) Command(args string) shellcmd.Command {
	return shellcmd.Command(fmt.Sprintf("%s %s", t.tmplData.FullCmd, args))
}

// WithFolder defines a custom folder path where the tool is expected to exist
// and where it should be installed if desired. Paths will be normalized to the
// operating system automatically, so unix-style paths are recommended.
func WithFolder(folder string) Option {
	return func(t *BinTool) error {
		t.folder = folder
		return nil
	}
}

// WithArchiveExt defines a custom extension to use when identifying an archive
// via the ArchiveExt template variable. The default archive extension is
// .tar.gz except for Windows, where it is .zip.
func WithArchiveExt(ext string) Option {
	return func(t *BinTool) error {
		t.tmplData.ArchiveExt = ext
		return nil
	}
}

// WithBinExt defines a custom extension to use when identifying a binary
// executable via the BinExt template variable. The default binary extension is
// empty for all operating systems except Windows, where it is .exe.
func WithBinExt(ext string) Option {
	return func(t *BinTool) error {
		t.tmplData.BinExt = ext
		return nil
	}
}

// WithVersionCmd defines a custom command used to test the version of the
// command for purposes of determining if the command is installed. The provided
// command is a template that can use any of the template parameters that are
// available to the url.
//
// The default test command is "{{.FullCmd}} --version".
func WithVersionCmd(cmd string) Option {
	return func(t *BinTool) error {
		t.versionCmd = cmd
		return nil
	}
}

func resolveTemplate(templateString string, tmplData templateData) (string, error) {
	tmpl, err := template.New("cmd").Parse(templateString)
	if err != nil {
		return "", fmt.Errorf("bintool: invalid template: %w", err)
	}

	var sb strings.Builder
	err = tmpl.Execute(&sb, tmplData)
	if err != nil {
		return "", fmt.Errorf("bintool: unable to execute template: %w", err)
	}

	return sb.String(), nil
}

func isAlphanumeric(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func downloadAndExtract(command string, url string) (data []byte, err error) {
	fmt.Printf("Downloading %s\n", command)

	res, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("bintool: unable to download executable: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("bintool: received unexpected response when downloading file: %d", res.StatusCode)
	}

	r, finish := progress(res.Body, res.ContentLength)
	defer finish()

	switch {
	case strings.HasSuffix(url, ".tar.gz"):
		return extractTarFile(r, command)
	case strings.HasSuffix(url, ".zip"):
		return extractZipFile(r, command)
	default:
		// Assume non-archive for others
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			return nil, fmt.Errorf("bintool: failed to retrieve body: %w", err)
		}

		return buf.Bytes(), nil
	}
}

func progress(r io.Reader, size int64) (io.Reader, func()) {
	if !isatty.IsTerminal(os.Stderr.Fd()) && !isatty.IsCygwinTerminal(os.Stderr.Fd()) {
		return r, func() {}
	}

	tmpl := `{{string . "prefix"}}{{counters . }}` +
		` {{bar . "[" "=" ">" " " "]" }} {{percent . }}` +
		` {{speed . "%s/s" }}{{string . "suffix"}}`

	bar := pb.New64(size).
		SetTemplate(pb.ProgressBarTemplate(tmpl)).
		SetRefreshRate(time.Second / 60).
		SetMaxWidth(100).
		Start()

	return bar.NewProxyReader(r), func() { bar.Finish() }
}

func extractTarFile(r io.Reader, filename string) (data []byte, err error) {
	var buf bytes.Buffer

	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("bintool: unable to read as gzip: %w", err)
	}

	tr := tar.NewReader(zr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("bintool: no executable found in archive")
		}

		if err != nil {
			return nil, fmt.Errorf("bintool: unable to read tar archive: %w", err)
		}

		name := filepath.Base(header.Name)

		if name != filename {
			continue
		}

		if _, err := io.Copy(&buf, tr); err != nil {
			return nil, fmt.Errorf("bintool: unable to copy executable out of archive: %w", err)
		}

		break
	}

	return buf.Bytes(), nil
}

func extractZipFile(r io.Reader, filename string) (data []byte, err error) {
	var rawBuf bytes.Buffer
	var buf bytes.Buffer
	if _, err := io.Copy(&rawBuf, r); err != nil {
		return nil, fmt.Errorf("bintool: unable to copy zip contents into buffer: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(rawBuf.Bytes()), int64(len(rawBuf.Bytes())))
	if err != nil {
		return nil, fmt.Errorf("bintool: unable to read as zip: %w", err)
	}

	var found bool
	for _, f := range zr.File {
		name := filepath.Base(f.Name)

		if name != filename {
			continue
		}

		fc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("bintool: unable to open executable in zip archive: %w", err)
		}

		defer fc.Close()

		if _, err := io.Copy(&buf, fc); err != nil {
			return nil, fmt.Errorf("bintool: unable to copy executable out of archive: %w", err)
		}

		found = true
		break
	}

	if !found {
		return nil, errors.New("bintool: no executable found in archive")
	}

	return buf.Bytes(), nil
}

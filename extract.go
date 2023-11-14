package main

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
)

func walkImage(img v1.Image, walkFn func(path string, isSymLink bool, actualPath string, openFn func() (fs.File, error)) error) error {
	tmpDir, err := os.MkdirTemp("", "image-ca-injector-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	err = extractImage(tmpDir, img)
	if err != nil {
		return err
	}

	return filepath.WalkDir(tmpDir, func(path string, d fs.DirEntry, err error) error {
		isSymLink := false
		actualPath := ""
		relPath, err := filepath.Rel(tmpDir, path)
		if err != nil {
			return err
		}
		relPath = "/" + relPath

		if d.Type()&os.ModeSymlink == os.ModeSymlink {
			isSymLink = true
			actualPath, err = filepath.EvalSymlinks(path)
			if err != nil {
				actualPath = ""
			} else {
				actualPath, err = filepath.Rel(tmpDir, actualPath)
				if err != nil {
					return err
				}
				actualPath = "/" + actualPath
			}
		}

		openFn := func() (fs.File, error) {
			path := path
			return os.Open(path)
		}

		err = walkFn(relPath, isSymLink, actualPath, openFn)
		if err != nil {
			return err
		}
		return nil
	})
}

func extractImage(targetDir string, img v1.Image) error {
	rc := mutate.Extract(img)
	err := untar(targetDir, rc)
	if err != nil {
		rc.Close()
		return err
	}
	return rc.Close()
}
func untar(dir string, input io.Reader) (err error) {
	tr := tar.NewReader(input)
	for {
		f, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar error: %w", err)
		}
		if !validRelPath(f.Name) {
			return fmt.Errorf("tar contained invalid name error %q", f.Name)
		}

		rel := filepath.FromSlash(f.Name)
		abs := filepath.Join(dir, rel)

		mode := f.FileInfo().Mode()

		// log.Printf("name=%s, mode=%s, linkname=%s, type=%q", f.Name, f.FileInfo().Mode(), f.Linkname, f.Typeflag)

		switch f.Typeflag {
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				return err
			}

			wf, err := os.OpenFile(abs, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode.Perm())
			if err != nil {
				return err
			}
			n, err := io.Copy(wf, tr)
			if closeErr := wf.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
			if err != nil {
				return fmt.Errorf("error writing to %s: %w", abs, err)
			}
			if n != f.Size {
				return fmt.Errorf("only wrote %d bytes to %s; expected %d", n, abs, f.Size)
			}
		case tar.TypeDir:
			if err := os.MkdirAll(abs, 0755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			linkName := f.Linkname
			if filepath.IsAbs(f.Linkname) {
				linkName = filepath.Join(dir, f.Linkname)
			}
			err := os.Symlink(linkName, abs)
			if err != nil {
				return err
			}

		case tar.TypeXGlobalHeader:
			// git archive generates these. Ignore them.
		default:
			return fmt.Errorf("tar file entry %s contained unsupported file type %v (%s, linkname=%s)", f.Name, mode, f.Typeflag, f.Linkname)
		}
	}

	return nil
}

func validRelativeDir(dir string) bool {
	if strings.Contains(dir, `\`) || path.IsAbs(dir) {
		return false
	}
	dir = path.Clean(dir)
	if strings.HasPrefix(dir, "../") || strings.HasSuffix(dir, "/..") || dir == ".." {
		return false
	}
	return true
}

func validRelPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, "../") {
		return false
	}
	return true
}

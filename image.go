package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type patchFn func(i *image) ([]v1.Layer, error)

func chainPatchFns(patches ...patchFn) patchFn {
	return func(i *image) ([]v1.Layer, error) {
		layers := []v1.Layer{}
		for _, patch := range patches {
			changes, err := patch(i)
			if err != nil {
				return nil, err
			}
			layers = append(layers, changes...)
		}
		return layers, nil
	}
}

func newLayer(hdr *tar.Header, modTime time.Time, content []byte) (v1.Layer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	newHdr := *hdr
	newHdr.Size = int64(len(content))
	newHdr.ModTime = modTime

	err := tw.WriteHeader(&newHdr)
	if err != nil {
		return nil, err
	}

	n, err := tw.Write(content)
	if err != nil {
		return nil, err
	}
	if int64(n) != newHdr.Size {
		return nil, fmt.Errorf("failed to write content into layer")
	}

	err = tw.Close()
	if err != nil {
		return nil, err
	}
	return static.NewLayer(buf.Bytes(), types.DockerLayer), nil
}

type image struct {
	tmpFile      *os.File
	tmpImage     v1.Image
	fileMetaData map[string]*tar.Header
}

func newImage(srcImg v1.Image) (*image, error) {

	var (
		err error
		i   = &image{}
	)

	i.tmpFile, err = os.CreateTemp("", "image-ca-injector-*.tar")
	if err != nil {
		return nil, err
	}

	err = tarball.Write(name.MustParseReference("tmp"), srcImg, i.tmpFile)
	if err != nil {
		i.close()
		return nil, err
	}

	tag, err := name.NewTag("tmp")
	if err != nil {
		i.close()
		return nil, err
	}

	i.tmpImage, err = tarball.ImageFromPath(i.tmpFile.Name(), &tag)
	if err != nil {
		i.close()
		return nil, err
	}

	i.fileMetaData, err = readMeta(i.tmpImage)
	if err != nil {
		i.close()
		return nil, err
	}
	return i, nil
}

func (i *image) files() []string {
	files := []string{}
	for path := range i.fileMetaData {
		files = append(files, path)
	}
	return files
}

func (i *image) image() v1.Image {
	return i.tmpImage
}

func (i *image) resolve(path string) (*tar.Header, bool) {
	file, ok := i.fileMetaData[path]
	if !ok {
		return nil, false
	}

	if file.Linkname == "" {
		return file, true
	}

	if filepath.IsAbs(file.Linkname) {
		linkPath := file.Linkname[1:]
		return i.resolve(linkPath)
	}

	linkPath := filepath.Join(filepath.Dir(path), file.Linkname)

	return i.resolve(linkPath)
}

func (i *image) getMeta(path string) (*tar.Header, bool) {
	file, ok := i.fileMetaData[path]
	return file, ok
}

func (i *image) open(path string) (io.ReadCloser, error) {
	file, ok := i.resolve(path)
	if !ok {
		return nil, fmt.Errorf("could not resolve link '%s'", path)
	}
	rc := mutate.Extract(i.tmpImage)
	return newTarFileReader(file.Name, rc)
}

func (i *image) close() error {
	err := i.tmpFile.Close()
	if err != nil {
		os.Remove(i.tmpFile.Name())
		return err
	}
	return os.Remove(i.tmpFile.Name())
}

type tarFileReader struct {
	closer io.Closer
	reader *tar.Reader
}

var _ io.ReadCloser = &tarFileReader{}

func newTarFileReader(path string, readCloser io.ReadCloser) (*tarFileReader, error) {
	found := false
	tr := tar.NewReader(readCloser)
	for {
		f, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			readCloser.Close()
			return nil, err
		}

		if f.Name == path {
			if f.Typeflag != tar.TypeReg {
				readCloser.Close()
				return nil, fmt.Errorf("path %s is not a regular file", path)
			}
			found = true
			break
		}
	}
	if !found {
		readCloser.Close()
		return nil, fmt.Errorf("file %s not found", path)
	}

	return &tarFileReader{
		closer: readCloser,
		reader: tr,
	}, nil
}

func (t *tarFileReader) Close() error {
	return t.closer.Close()
}

func (t *tarFileReader) Read(p []byte) (int, error) {
	return t.reader.Read(p)
}

func readMeta(img v1.Image) (map[string]*tar.Header, error) {
	files := map[string]*tar.Header{}

	rc := mutate.Extract(img)
	tr := tar.NewReader(rc)

	for {
		f, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			rc.Close()
			return nil, err
		}
		files[f.Name] = f
	}
	return files, rc.Close()
}

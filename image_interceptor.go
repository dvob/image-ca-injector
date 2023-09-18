package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

var _ v1.Image = &imageFileCollector{}

type imageFileCollector struct {
	v1.Image
}

func (i *imageFileCollector) Layers() ([]v1.Layer, error) {
	layers := []v1.Layer{}
	origLayers, err := i.Image.Layers()
	if err != nil {
		return nil, err
	}
	for _, layer := range origLayers {
		layers = append(layers, &layerFileCollector{layer})
	}
	return layers, nil
}

var _ v1.Layer = &layerFileCollector{}

type layerFileCollector struct {
	v1.Layer
}

func (l *layerFileCollector) Compressed() (io.ReadCloser, error) {
	mediaType, err := l.MediaType()
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(string(mediaType), "gzip") {
		return nil, fmt.Errorf("unsupported compression")
	}
	rc, err := l.Layer.Compressed()
	if err != nil {
		return nil, err
	}
	return newTARFileCollector(rc), nil
}

func (l *layerFileCollector) Uncompressed() (io.ReadCloser, error) {
	return nil, fmt.Errorf("no implemented")
	rc, err := l.Layer.Uncompressed()
	if err != nil {
		return nil, err
	}
	return newTARFileCollector(rc), nil
}

type tarFileCollector struct {
	input     io.ReadCloser
	teeReader io.Reader
	callback  func(name string)
	err       error
}

func (tfc *tarFileCollector) Close() error {
	// TODO: close pipe etc.
	return tfc.input.Close()
}

func (tfc *tarFileCollector) Read(p []byte) (int, error) {
	return tfc.teeReader.Read(p)
}

func newTARFileCollector(input io.ReadCloser) io.ReadCloser {
	tf := &tarFileCollector{}
	tf.input = input
	tf.callback = func(name string) {
		fmt.Println("-", name)
	}

	pr, wr := io.Pipe()
	tf.teeReader = io.TeeReader(input, wr)
	go func() {
		gzipReader, err := gzip.NewReader(pr)
		if err != nil {
			wr.CloseWithError(err)
			tf.err = err
			return
		}
		defer gzipReader.Close()
		tarReader := tar.NewReader(gzipReader)
		for {
			hdr, err := tarReader.Next()
			if err == io.EOF {
				break // End of archive
			}
			if err != nil {
				wr.CloseWithError(err)
				tf.err = err
				return
			}
			tf.callback(hdr.Name)
		}
		wr.Close()
	}()

	return tf
}

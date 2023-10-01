package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/logs"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type file struct {
	hdr     *tar.Header
	content []byte
}

func run() error {
	if len(os.Args) == 3 && os.Args[1] == "hash" {
		rawData, err := os.ReadFile(os.Args[2])
		if err != nil {
			return err
		}

		hash, err := getOpenSSLHash(rawData)
		if err != nil {
			return err
		}
		fmt.Println(hash)
		return nil
	}
	if len(os.Args) < 4 {
		return fmt.Errorf("missing arguments")
	}

	logs.Progress.SetOutput(os.Stderr)
	caFile, err := os.ReadFile(os.Args[3])
	if err != nil {
		return err
	}

	srcRef, err := name.ParseReference(os.Args[1])
	if err != nil {
		return err
	}

	dstRef, err := name.ParseReference(os.Args[2])
	if err != nil {
		return err
	}

	srcImg, err := remote.Image(srcRef)
	if err != nil {
		return err
	}

	digest, err := srcImg.Digest()
	if err != nil {
		return err
	}
	dstShaRef := dstRef.Context().Digest(digest.String())

	log.Print("sync image to", dstShaRef.String())
	err = remote.Write(dstShaRef, srcImg)
	if err != nil {
		return err
	}

	dstShaImg, err := remote.Image(dstShaRef)
	if err != nil {
		return err
	}

	log.Print("prepare CA")

	files := map[string]file{}

	// TODO:
	//  - links not handled
	//  - deletions not handled (.wh. whiteout files)
	err = walkFiles(dstShaImg, func(digest string, hdr *tar.Header, r io.Reader) error {
		path := filepath.Clean(hdr.Name)
		if !filepath.IsAbs(path) {
			path = "/" + path
		}

		if hdr.Typeflag != tar.TypeReg {
			return nil
		}
		if isTruststore(path) {
			content, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			files[path] = file{
				content: content,
				hdr:     hdr,
			}
		}
		return nil
	})
	if err != nil {
		return nil
	}

	layers := []v1.Layer{}
	for _, file := range files {
		layers = append(layers, newLayerWithCA(file, caFile))
	}

	newImg, err := mutate.AppendLayers(dstShaImg, layers...)
	if err != nil {
		return err
	}

	remote.Write(dstRef, newImg)
	return nil
}

func newLayerWithCA(file file, caFile []byte) v1.Layer {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	newTrustStore := append(file.content, caFile...)
	newHdr := *file.hdr
	newHdr.Size = int64(len(newTrustStore))
	tw.WriteHeader(&newHdr)
	tw.Write(newTrustStore)
	return stream.NewLayer(io.NopCloser(&buf))
}

func printFn(_ string, hdr *tar.Header, _ io.Reader) error {
	out, err := json.Marshal(hdr)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func walkFiles(img v1.Image, walkFn func(layerDigest string, h *tar.Header, r io.Reader) error) error {
	layers, err := img.Layers()
	if err != nil {
		return err
	}
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		err = walkLayerFiles(layer, walkFn)
		if err != nil {
			return err
		}
	}
	return nil
}

func walkLayerFiles(l v1.Layer, walkFn func(layerDigest string, h *tar.Header, r io.Reader) error) error {
	rc, err := l.Uncompressed()
	if err != nil {
		return err
	}

	digest, err := l.Digest()
	if err != nil {
		return err
	}

	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return err
		}
		err = walkFn(digest.String(), hdr, tr)
		if err != nil {
			return err
		}
	}
	return rc.Close()
}

func isTruststore(path string) bool {
	for _, certFile := range certFiles {
		if path == certFile {
			return true
		}
	}

	// for _, certDir := range certDirectories {
	// 	if path == certDir {
	// 		return true
	// 	}
	// }
	return false
}

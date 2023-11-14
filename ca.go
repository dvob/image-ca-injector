package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/pavlo-v-chernykh/keystore-go/v4"
)

func getCALayers(srcImg v1.Image, caPEM []byte) ([]v1.Layer, error) {
	type file struct {
		// jks or pem
		kind    string
		path    string
		content []byte
		hdr     *tar.Header
	}

	files := map[string]file{}

	err := walkImage(srcImg, func(path string, isSymLink bool, actualPath string, openFn func() (fs.File, error)) error {
		log.Printf("path=%s, is_symlink=%t, actual_path=%s\n", path, isSymLink, actualPath)
		if isPEMTruststore(path) {
			log.Printf("path %s is pem truststore", path)
			f, err := openFn()
			if err != nil {
				return err
			}
			content, err := io.ReadAll(f)
			files[path] = file{
				kind:    "pem",
				path:    path,
				content: content,
				hdr:     &tar.Header{},
			}
		} else if isJKSTruststore(path) {
			log.Printf("path %s is jks truststore", path)
			f, err := openFn()
			if err != nil {
				return err
			}
			content, err := io.ReadAll(f)
			files[path] = file{
				kind:    "jks",
				path:    path,
				content: content,
				hdr:     &tar.Header{},
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk: %w", err)
	}

	layers := []v1.Layer{}
	for _, store := range files {
		switch store.kind {
		case "pem":
			log.Printf("path=%s, type=%s", store.path, store.kind)
			newContent, err := newPEMTruststore(store.content, caPEM)
			if err != nil {
				return nil, err
			}
			layer, err := newLayer(store.path, newContent)
			if err != nil {
				return nil, err
			}
			layers = append(layers, layer)

		case "jks":
			log.Printf("path=%s, type=%s", store.path, store.kind)
			newContent, err := newJKSTruststore(store.content, caPEM)
			if err != nil {
				return nil, err
			}
			layer, err := newLayer(store.path, newContent)
			if err != nil {
				return nil, err
			}
			layers = append(layers, layer)
		default:
			return nil, fmt.Errorf("unknown truststore type '%s'", store.kind)
		}
	}

	return layers, nil
}

func newLayer(path string, content []byte) (*stream.Layer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     path,
		Size:     int64(len(content)),
	}
	tw.WriteHeader(hdr)
	tw.Write(content)
	err := tw.Close()
	if err != nil {
		return nil, err
	}
	return stream.NewLayer(io.NopCloser(&buf)), nil
}

func isPEMTruststore(path string) bool {
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

func newPEMTruststore(currentFile []byte, caFile []byte) ([]byte, error) {
	return append(currentFile, caFile...), nil
}

func isJKSTruststore(path string) bool {
	if strings.Contains(path, "lib/security/cacerts") {
		return true
	}
	return false
}

func newJKSTruststore(currentFile []byte, caFile []byte) ([]byte, error) {
	ks := keystore.New()
	err := ks.Load(bytes.NewBuffer(currentFile), []byte("changeit"))
	if err != nil {
		return nil, fmt.Errorf("failed to load java key store: %w", err)
	}

	err = ks.SetTrustedCertificateEntry("ouralias", keystore.TrustedCertificateEntry{
		CreationTime: time.Now(),
		Certificate: keystore.Certificate{
			Type:    "X509",
			Content: caFile,
		},
	})
	if err != nil {
		return nil, err
	}

	newJKS := &bytes.Buffer{}
	err = ks.Store(newJKS, []byte("changeit"))
	if err != nil {
		return nil, err
	}
	return newJKS.Bytes(), nil
}

package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/pavel-v-chernykh/keystore-go/v4"
)

func patchJKSTruststore(pem []byte) patchFn {
	return func(i *image) ([]v1.Layer, error) {
		truststores := map[string]*tar.Header{}
		for _, path := range i.files() {
			if !strings.HasSuffix(path, "/lib/security/cacerts") {
				continue
			}
			hdr, ok := i.resolve(path)
			if !ok {
				slog.Info("cant resolve %s", path)
				continue
			}
			truststores[hdr.Name] = hdr
		}

		layers := []v1.Layer{}
		now := time.Now()
		for path, hdr := range truststores {
			slog.Info("prepare JKS truststore", "file", path)
			r, err := i.open(path)
			if err != nil {
				return nil, err
			}
			defer r.Close()
			oldContent, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			newContent, err := newJKSTruststore(oldContent, pem)
			if err != nil {
				return nil, err
			}

			layer, err := newLayer(hdr, now, newContent)
			if err != nil {
				return nil, err
			}
			layers = append(layers, layer)
		}
		return layers, nil
	}
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

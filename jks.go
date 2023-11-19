package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/dvob/pcert"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/pavel-v-chernykh/keystore-go/v4"
	"software.sslmate.com/src/go-pkcs12"
)

func patchJKSTruststore(name string, pem []byte) patchFn {
	return func(i *image) ([]v1.Layer, error) {
		truststores := map[string]*tar.Header{}
		for _, path := range i.files() {
			if !strings.HasSuffix(path, "/lib/security/cacerts") {
				continue
			}
			hdr, ok := i.resolve(path)
			if !ok {
				slog.Info("cant resolve link", "path", path)
				continue
			}
			truststores[hdr.Name] = hdr
		}

		layers := []v1.Layer{}
		now := time.Now()
		for path, hdr := range truststores {
			slog.Info("prepare java truststore", "file", path)
			r, err := i.open(path)
			if err != nil {
				return nil, err
			}
			defer r.Close()
			oldContent, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			newContent, err := newJKSTruststore(oldContent, name, pem)
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

func newPKCS12Truststore(currentFile []byte, caFile []byte) ([]byte, error) {
	cert, err := pcert.Parse(caFile)
	if err != nil {
		return nil, err
	}
	certs, err := pkcs12.DecodeTrustStore(currentFile, "")
	if err != nil {
		return nil, err
	}
	certs = append(certs, cert)

	return pkcs12.Passwordless.EncodeTrustStore(certs, "")
}

func newJKSTruststore(currentFile []byte, name string, caFile []byte) ([]byte, error) {
	ks := keystore.New()
	err := ks.Load(bytes.NewBuffer(currentFile), []byte("changeit"))
	if err != nil {
		if err.Error() == "got invalid magic" {
			return newPKCS12Truststore(currentFile, caFile)
		}
		return nil, fmt.Errorf("failed to load java key store: %w", err)
	}

	err = ks.SetTrustedCertificateEntry(name, keystore.TrustedCertificateEntry{
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

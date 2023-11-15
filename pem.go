package main

import (
	"archive/tar"
	"io"
	"log/slog"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func patchPEMTruststore(pem []byte) patchFn {
	return func(i *image) ([]v1.Layer, error) {
		truststores := map[string]*tar.Header{}
		for _, certFile := range certFiles {
			certFile := certFile[1:]
			hdr, ok := i.resolve(certFile)
			if !ok {
				continue
			}
			truststores[hdr.Name] = hdr
		}

		layers := []v1.Layer{}
		now := time.Now()
		for path, hdr := range truststores {
			slog.Info("prepare PEM truststore", "file", path)
			r, err := i.open(path)
			if err != nil {
				return nil, err
			}
			defer r.Close()
			oldContent, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			layer, err := newLayer(hdr, now, append(oldContent, pem...))
			if err != nil {
				return nil, err
			}
			layers = append(layers, layer)
		}
		return layers, nil
	}
}

package main

import (
	"archive/tar"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
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

var customCertLocations = map[string]string{
	"/etc/pki/ca-trust/source/anchors":          "%s.pem",
	"/usr/local/share/ca-certificates":          "%s.crt",
	"/etc/ca-certificates/trust-source/anchors": "%s.crt",
	"/usr/share/pki/trust/anchors":              "%s.pem",
}

var distroPathes = map[string]string{
	"alpine": "/usr/local/share/ca-certificates",
}

func putPEMTruststore(name string, pem []byte) patchFn {
	return func(i *image) ([]v1.Layer, error) {

		layers := []v1.Layer{}
		now := time.Now()
		for path, fileFormat := range customCertLocations {
			_, ok := i.getMeta(path[1:])
			if !ok {
				continue
			}

			fileName := fmt.Sprintf(fileFormat, name)
			filePath := filepath.Join(path[1:], fileName)

			hdr := &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     filePath,
				Size:     int64(len(pem)),
				Mode:     0644,
				Uid:      0,
				Gid:      0,
				ModTime:  now,
			}

			slog.Info("add custom PEM truststore", "file", hdr.Name)
			layer, err := newLayer(hdr, now, pem)
			if err != nil {
				return nil, err
			}
			layers = append(layers, layer)

		}

		if len(layers) != 0 {
			return layers, nil
		}

		// try to detect distro

		osInfo := getOSInfo(i)
		if osInfo == nil {
			return layers, nil
		}

		path, ok := distroPathes[osInfo.Vendor]
		if !ok {
			return layers, nil
		}

		fileFormat := customCertLocations[path]

		fileName := fmt.Sprintf(fileFormat, name)
		filePath := filepath.Join(path[1:], fileName)

		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     filePath,
			Size:     int64(len(pem)),
			Mode:     0644,
			Uid:      0,
			Gid:      0,
			ModTime:  now,
		}
		slog.Info("add custom PEM truststore for detected OS", "os", osInfo.Vendor, "file", hdr.Name, "path", path)

		layer, err := newLayer(hdr, now, pem)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)

		return layers, nil
	}
}

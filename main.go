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
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/logs"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/pavlo-v-chernykh/keystore-go/v4"
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

	// read CA file to add
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

	log.Printf("use tag '%s'", digest.String())
	var tag name.Tag = dstShaRef.Tag("tmp")
	log.Print("sync image to", dstShaRef.String())
	id, err := daemon.Write(tag, srcImg)
	//err = remote.Write(dstShaRef, srcImg)
	if err != nil {
		return err
	}
	log.Printf("written id '%s'", id)

	//dstShaImg, err := remote.Image(dstShaRef)
	dstShaImg, err := daemon.Image(tag)
	if err != nil {
		return err
	}

	log.Print("prepare CA")

	files := map[string]*file{}

	jksKeyStores := map[string]*file{}

	// TODO:
	//  - links not handled
	//  - deletions not handled (.wh. whiteout files)
	err = walkFiles(dstShaImg, func(digest string, hdr *tar.Header, r io.Reader) error {
		path := filepath.Clean(hdr.Name)
		if !filepath.IsAbs(path) {
			path = "/" + path
		}

		if f, ok := files[path]; ok && f == nil {
			if hdr.Typeflag != tar.TypeReg {
				log.Printf("remembered file is not regular: %s", path)
			} else {
				content, err := io.ReadAll(r)
				if err != nil {
					return err
				}
				files[path] = &file{
					content: content,
					hdr:     hdr,
				}
			}
		}
		if f, ok := jksKeyStores[path]; ok && f == nil {
			if hdr.Typeflag != tar.TypeReg {
				log.Printf("remembered jks is not regular: %s", path)
			} else {
				content, err := io.ReadAll(r)
				if err != nil {
					return err
				}
				jksKeyStores[path] = &file{
					content: content,
					hdr:     hdr,
				}
			}
		}

		if isTruststore(path) {
			if hdr.Typeflag == tar.TypeSymlink {
				realLocation := hdr.Linkname
				log.Printf("remember %s %s to read", path, realLocation)
				files[path] = nil
			} else if hdr.Typeflag == tar.TypeReg {
				content, err := io.ReadAll(r)
				if err != nil {
					return err
				}
				files[path] = &file{
					content: content,
					hdr:     hdr,
				}
			} else {
				log.Printf("matched file not supported %s %b", path, hdr.Typeflag)
			}
		}
		if strings.HasSuffix(path, "lib/security/cacerts") {
			if hdr.Typeflag == tar.TypeSymlink {
				realLocation := hdr.Linkname
				log.Printf("remember %s %s to read", path, realLocation)
				jksKeyStores[path] = nil
			} else if hdr.Typeflag == tar.TypeReg {
				content, err := io.ReadAll(r)
				if err != nil {
					return err
				}
				jksKeyStores[path] = &file{
					content: content,
					hdr:     hdr,
				}
			} else {
				log.Printf("matched file not supported %s %b", path, hdr.Typeflag)
			}
		}
		return nil
	})
	if err != nil {
		return nil
	}

	layers := []v1.Layer{}
	for path, file := range files {
		log.Printf("prepare new ca pem file %s", path)
		if file == nil {
			log.Printf("file %s was not read", path)
			continue
		}
		layers = append(layers, newLayerWithCA(file, caFile))
	}

	for path, jksKeyStore := range jksKeyStores {
		log.Printf("prepare new jks file %s", path)
		if jksKeyStore == nil {
			log.Printf("file %s was not read", path)
			continue
		}
		layer, err := newLayerWithJKS(jksKeyStore, caFile)
		if err != nil {
			return err
		}
		layers = append(layers, layer)
	}

	newImg, err := mutate.AppendLayers(dstShaImg, layers...)
	if err != nil {
		return err
	}

	target := dstRef.Context().Tag(dstRef.Identifier())
	log.Printf("Write to %s", target)
	daemon.Write(target, newImg)
	return nil
}

func newLayerWithJKS(file *file, caFile []byte) (v1.Layer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	ks := keystore.New()
	err := ks.Load(bytes.NewBuffer(file.content), []byte("changeit"))
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

	newHdr := *file.hdr
	newHdr.Size = int64(newJKS.Len())
	tw.WriteHeader(&newHdr)
	tw.Write(newJKS.Bytes())
	return stream.NewLayer(io.NopCloser(&buf)), nil
}

func newLayerWithCA(file *file, caFile []byte) v1.Layer {
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
	// layers, err := img.Layers()
	// if err != nil {
	// 	return err
	// }
	// for i := len(layers) - 1; i >= 0; i-- {
	// 	layer := layers[i]
	// 	err = walkLayerFiles(layer, walkFn)
	// 	if err != nil {
	// 		return err
	// 	}
	// }
	layers, err := img.Layers()
	if err != nil {
		return err
	}
	log.Println("walk %d layers", len(layers))
	for _, layer := range layers {
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

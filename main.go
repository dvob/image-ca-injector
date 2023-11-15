package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/logs"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var (
		opts = &opts{
			srcType: "remote",
			dstType: "remote",
		}
	)

	flag.StringVar(&opts.srcType, "src", opts.srcType, "source type (remote, docker, tar)")
	flag.StringVar(&opts.dstType, "dst", opts.dstType, "destination type (remote, docker, tar)")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s SOURCE DESTINATION CA_FILE:\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() < 3 {
		return fmt.Errorf("missing arguments. want SOURCE DESTINATION CAFILE")
	}

	opts.src = flag.Arg(0)
	opts.dst = flag.Arg(1)
	opts.caFile = flag.Arg(2)

	logs.Progress.SetOutput(os.Stderr)

	return injectCA(opts)

}

type opts struct {
	src     string
	dst     string
	srcType string
	dstType string
	caFile  string
}

func injectCA(opts *opts) error {
	caPEM, err := os.ReadFile(opts.caFile)
	if err != nil {
		return err
	}

	slog.Info("read image", "src", opts.src, "src_type", opts.srcType)
	srcImg, err := getImage(opts.srcType, opts.src)
	if err != nil {
		return err
	}

	image, err := newImage(srcImg)
	if err != nil {
		return err
	}
	defer image.close()

	patch := chainPatchFns(
		patchPEMTruststore(caPEM),
		patchJKSTruststore(caPEM),
	)

	slog.Info("prepare truststore patches")
	layers, err := patch(image)
	if err != nil {
		return fmt.Errorf("failed to prepare patches: %w", err)
	}

	newImg, err := mutate.AppendLayers(image.image(), layers...)
	if err != nil {
		return fmt.Errorf("failed to append layers: %w", err)
	}

	if opts.srcType == "remote" || opts.srcType == "docker" {
		annotations := map[string]string{}
		digest, err := srcImg.Digest()
		if err != nil {
			slog.Warn("failed to obtain digest from source image", "err", err)
		} else {
			annotations["org.opencontainers.image.base.digest"] = digest.String()
		}

		ref, err := name.ParseReference(opts.src)
		if err != nil {
			return err
		}
		annotations["org.opencontainers.image.base.image"] = ref.Name()

		// https://github.com/opencontainers/image-spec/blob/main/annotations.md
		newImg = mutate.Annotations(newImg, annotations).(v1.Image)
	}

	slog.Info("write image", "dst", opts.dst, "dst_type", opts.dstType)
	err = putImage(opts.dstType, opts.dst, newImg)
	if err != nil {
		return fmt.Errorf("failed to write tar: %w", err)
	}
	return nil

}

func putImage(typ string, location string, img v1.Image) error {
	switch typ {
	case "remote":
		ref, err := name.ParseReference(location)
		if err != nil {
			return err
		}
		return remote.Write(ref, img, makeOptions()...)

	case "docker":
		tag, err := name.NewTag(location)
		if err != nil {
			return err
		}
		_, err = daemon.Write(tag, img)
		return err

	case "file":
		return tarball.WriteToFile(location, &name.Tag{}, img)

	default:
		return fmt.Errorf("unknown type '%s'", typ)

	}
}

func getImage(typ string, location string) (v1.Image, error) {

	switch typ {
	case "remote":
		ref, err := name.ParseReference(location)
		if err != nil {
			return nil, err
		}
		return remote.Image(ref, makeOptions()...)

	case "docker":
		ref, err := name.ParseReference(location)
		if err != nil {
			return nil, err
		}
		return daemon.Image(ref)

	case "file":
		return tarball.ImageFromPath(location, &name.Tag{})

	default:
		return nil, fmt.Errorf("unknown type '%s'", typ)

	}
}

func makeOptions() []remote.Option {
	return []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
}

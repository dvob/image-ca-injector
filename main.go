package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/google/go-containerregistry/pkg/logs"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	if flag.NArg() < 3 {
		return fmt.Errorf("missing arguments. want SOURCE DESTINATION CAFILE")
	}

	logs.Progress.SetOutput(os.Stderr)

	source := flag.Arg(0)
	destination := flag.Arg(1)
	caFile := flag.Arg(2)

	srcRef, err := name.ParseReference(source)
	if err != nil {
		return err
	}

	dstRef, err := name.ParseReference(destination)
	if err != nil {
		return err
	}

	srcImg, err := remote.Image(srcRef)
	if err != nil {
		return err
	}

	caPEM, err := os.ReadFile(caFile)
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

	layers, err := patch(image)
	if err != nil {
		return fmt.Errorf("failed to construct layers: %w", err)
	}

	// add layers
	newImg, err := mutate.AppendLayers(image.image(), layers...)
	if err != nil {
		return fmt.Errorf("failed to append layers: %w", err)
	}

	target := dstRef.Context().Tag(dstRef.Identifier())
	log.Printf("Write to %s", target)
	_, err = daemon.Write(target, newImg)
	//err = tarball.WriteToFile("myimage.tar", dstRef, newImg)
	if err != nil {
		return fmt.Errorf("failed to write tar: %w", err)
	}
	return nil

}

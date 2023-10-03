package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pavlo-v-chernykh/keystore-go/v4"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ks := keystore.New()

	// cert, err := pcert.Load(os.Args[1])
	// if err != nil {
	// 	return err
	// }

	// pem := pcert.Encode(cert)
	pemFile := os.Args[1]
	jksFile := os.Args[2]

	pem, err := os.ReadFile(pemFile)
	if err != nil {
		return err
	}

	f, err := os.Open(jksFile)
	if err != nil {
		return err
	}

	err = ks.Load(f, []byte("changeit"))
	if err != nil {
		return err
	}
	f.Close()

	alias := filepath.Base(pemFile)[:len(filepath.Ext(pemFile))]
	alias = strings.ToLower(alias)

	ks.SetTrustedCertificateEntry(alias, keystore.TrustedCertificateEntry{
		CreationTime: time.Now(),
		Certificate: keystore.Certificate{
			Type:    "X509",
			Content: pem,
		},
	})

	outFile, err := os.Create(jksFile)
	if err != nil {
		return err
	}

	ks.Store(outFile, []byte("changeit"))

	return nil
}

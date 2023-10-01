package main

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"strings"
)

type AttributeTypeAndUTF8Value struct {
	Type  asn1.ObjectIdentifier
	Value string `asn1:"utf8"`
}

type RelativeDistinguishedNameSET []AttributeTypeAndUTF8Value

type RDNSequence []RelativeDistinguishedNameSET

func getOpenSSLHash(pemData []byte) (string, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return "", fmt.Errorf("no pem data")
	}
	if block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("expected CERTIFICATE got %s", block.Type)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	canonicalName, err := getCanonicalName(cert)
	if err != nil {
		return "", err
	}
	// TODO: from where are the additional bytes
	return openSSLHash(canonicalName[2:]), nil
}

func getCanonicalName(cert *x509.Certificate) ([]byte, error) {
	rdnSequence := RDNSequence{}

	_, err := asn1.Unmarshal(cert.RawSubject, &rdnSequence)
	if err != nil {
		return nil, err
	}

	canonicalizeRDNSequence(rdnSequence)

	outBytes, err := asn1.Marshal(rdnSequence)
	if err != nil {
		return nil, err
	}
	return outBytes, err
}

// https://github.com/openssl/openssl/blob/852c2ed260860b6b85c84f9fe96fb4d23d49c9f2/crypto/x509/x_name.c#L296-L306
func canonicalizeRDNSequence(rdnSequence RDNSequence) {
	for i := range rdnSequence {
		for j := range rdnSequence[i] {
			value := rdnSequence[i][j].Value
			// value, ok := item.Value.(string)
			// if !ok {
			// 	continue
			// }

			// TODO: remove whitespaces etc.
			rdnSequence[i][j].Value = strings.ToLower(value)
		}
	}
}

// https://github.com/openssl/openssl/blob/8ed76c62b5d3214e807e684c06efd69c6471c800/crypto/x509/x509_cmp.c#L302
func openSSLHash(data []byte) string {
	h := sha1.New()
	h.Write(data)
	hash := h.Sum(nil)
	max := ^uint32(0)
	truncHash := (uint32(hash[0]) | uint32(hash[1])<<uint32(8) |
		uint32(hash[2])<<uint32(16) | uint32(hash[3])<<uint32(24)) & max
	return fmt.Sprintf("%08x", truncHash)
}

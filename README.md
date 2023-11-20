# image-ca-injector

The image-ca-injector copies a image from a source registry into a destination registry and adds a CA to the system truststore.
```
image-ca-injector alpine registry.mycompany.com/alpine your_company_ca.crt
```

For this it performs the follwing steps:
* Download the image into a local temporary tar file
* Find PEM truststore files (based on [root_linux.go](https://github.com/golang/go/blob/c05fceb73cafd642d26660148357a4f60172aa1a/src/crypto/x509/root_linux.go)) and add the specified CA to it.
  * Debian/Ubuntu/Gentoo etc.: `/etc/ssl/certs/ca-certificates.crt`
  * Fedora/RHEL 6: `/etc/pki/tls/certs/ca-bundle.crt`
  * OpenSUSE: `/etc/ssl/ca-bundle.pem`
  * OpenELEC: `/etc/pki/tls/cacert.pem`
  * CentOS/RHEL 7: `/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem`
  * Alpine Linux: `/etc/ssl/cert.pem`
* Look for common places for custom CAs and put the CA there:
  * `/usr/local/share/ca-certificates/`
  * `/etc/pki/ca-trust/source/anchors/`
  * `/etc/ca-certificates/trust-source/anchors/`
  * `/usr/share/pki/trust/anchors/`
* Find JKS truststore files (`*/lib/security/cacerts`) and add the specified CA to it.
* Upload the image to destination

## Install
```
go install github.com/dvob/image-ca-injector@latest
```

## Usage
```
image-ca-injector SOURCE DESTINATION CA-FILE
```

## Examples
```
image-ca-injector docker.index.io/alpine registry.mycompany.com/alpine ca.crt
```

Where `ca.crt` is a PEM encoded certificate like this:
```
-----BEGIN CERTIFICATE-----
MIIBWjCCAQGgAwIBAgIRAMu4py77kY5FVUdasZ+9tJYwCgYIKoZIzj0EAwIwDTEL
MAkGA1UEAxMCY2EwHhcNMjMwOTE4MTQwMjAwWhcNMjQwOTE3MTQwMjAwWjANMQsw
CQYDVQQDEwJjYTBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABH3oPQNdbQwloD0u
NylspowYas1GQde2zQOtjYEhyBPVSC09uQE64P7XH5SiH/8JuJZk2sR3l7AMGodP
Df1Zm/qjQjBAMA4GA1UdDwEB/wQEAwIBBjAPBgNVHRMBAf8EBTADAQH/MB0GA1Ud
DgQWBBSmC21mUOJYl/ola0zeP8B837UnrjAKBggqhkjOPQQDAgNHADBEAiBNQJ+f
sX9bA4D6j7clcKIZnH3UZT7EZ6bzLYEHinnncgIgTVdSzkDeRPbTDF/EyTTVg/tS
eNR2QnBwV13+5KYhcyQ=
-----END CERTIFICATE-----
```

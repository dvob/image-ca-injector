# image-ca-injector

The image-ca-injector copies a image from a source registry into a destination registry and adds a CA to the system truststore.

For this it performs the follwing steps:
* Copy the image to the destination repository
* Find truststore files (based on [root_linux.go](https://github.com/golang/go/blob/c05fceb73cafd642d26660148357a4f60172aa1a/src/crypto/x509/root_linux.go))
  * Debian/Ubuntu/Gentoo etc.: `/etc/ssl/certs/ca-certificates.crt`
  * Fedora/RHEL 6: `/etc/pki/tls/certs/ca-bundle.crt`
  * OpenSUSE: `/etc/ssl/ca-bundle.pem`
  * OpenELEC: `/etc/pki/tls/cacert.pem`
  * CentOS/RHEL 7: `/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem`
  * Alpine Linux: `/etc/ssl/cert.pem`
* If one of these files are found create a new layer which adds our own CA to the truststore.

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

## Caveats
* Links are not handeld: If the truststore file is a link the CA is not added
* [Directories](https://github.com/golang/go/blob/c05fceb73cafd642d26660148357a4f60172aa1a/src/crypto/x509/root_linux.go#L18) are not handled
* If you use system tools (e.g. `update-ca-certificates`) to update the truststore after image-ca-injector ran its likely that the added certificate gets removed
* Image gets downloaded twice. First from source to upload it to the destination and then from the destination to inspect the files in the image to find the truststores. Maybe this could be improved (see `image_interceptor.go`) but it would also get complicated for cases where certain layers are already available.

package main

// adapted from: https://github.com/zcalusic/sysinfo/blob/30169cfb37112a562cbf9133494a323764ad852c/os.go#L32

import (
	"io"
	"regexp"
	"strings"
)

type osInfo struct {
	Name         string `json:"name,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
	Version      string `json:"version,omitempty"`
	Release      string `json:"release,omitempty"`
	Architecture string `json:"architecture,omitempty"`
}

var (
	rePrettyName = regexp.MustCompile(`^PRETTY_NAME=(.*)$`)
	reID         = regexp.MustCompile(`^ID=(.*)$`)
	reVersionID  = regexp.MustCompile(`^VERSION_ID=(.*)$`)
	reUbuntu     = regexp.MustCompile(`[\( ]([\d\.]+)`)
	reCentOS     = regexp.MustCompile(`^CentOS( Linux)? release ([\d\.]+)`)
	reRedHat     = regexp.MustCompile(`[\( ]([\d\.]+)`)
)

func readFile(img *image, path string) string {
	rc, err := img.open(path[1:])
	if err != nil {
		return ""
	}

	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func fileExists(img *image, path string) bool {
	path = path[1:]
	_, ok := img.getMeta(path)
	return ok
}

func getOSInfo(img *image) *osInfo {
	oi := &osInfo{}
	// This seems to be the best and most portable way to detect OS architecture (NOT kernel!)
	if fileExists(img, "/lib64/ld-linux-x86-64.so.2") {
		oi.Architecture = "amd64"
	} else if fileExists(img, "/lib/ld-linux.so.2") {
		oi.Architecture = "i386"
	}

	osRelease := readFile(img, "/etc/os-release")
	if osRelease == "" {
		return nil
	}

	for _, line := range strings.Split(osRelease, "\n") {
		if m := rePrettyName.FindStringSubmatch(line); m != nil {
			oi.Name = strings.Trim(m[1], `"`)
		} else if m := reID.FindStringSubmatch(line); m != nil {
			oi.Vendor = strings.Trim(m[1], `"`)
		} else if m := reVersionID.FindStringSubmatch(line); m != nil {
			oi.Version = strings.Trim(m[1], `"`)
		}
	}

	switch oi.Vendor {
	case "debian":
		oi.Release = readFile(img, "/etc/debian_version")
	case "ubuntu":
		if m := reUbuntu.FindStringSubmatch(oi.Name); m != nil {
			oi.Release = m[1]
		}
	case "centos":
		release := readFile(img, "/etc/centos-release")
		if release != "" {
			if m := reCentOS.FindStringSubmatch(release); m != nil {
				oi.Release = m[2]
			}
		}
	case "rhel":
		release := readFile(img, "/etc/redhat-release")
		if release != "" {
			if m := reRedHat.FindStringSubmatch(release); m != nil {
				oi.Release = m[1]
			}
		}
		if oi.Release == "" {
			if m := reRedHat.FindStringSubmatch(oi.Name); m != nil {
				oi.Release = m[1]
			}
		}
	}
	return oi
}

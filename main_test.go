package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dvob/pcert"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

func TestAddCA(t *testing.T) {
	var (
		testCertDir = "test-certs"
		networkName = "image-ca-injector-test"
		serverName  = "test.example.com"
	)

	_, skipCleanup := os.LookupEnv("SKIP_CLEANUP")
	if skipCleanup {
		t.Log("skip cleanup")
	}

	err := os.RemoveAll(testCertDir)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(testCertDir, 0755)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if skipCleanup {
			return
		}
		err := os.RemoveAll(testCertDir)
		if err != nil {
			t.Log("falied to cleanup test certs", err)
		}
	}()

	caCertPEM, caKeyPEM, err := pcert.Create(pcert.NewCACertificate("myca"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(testCertDir, "myca.crt"), caCertPEM, 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(testCertDir, "myca.key"), caKeyPEM, 0644)
	if err != nil {
		t.Fatal(err)
	}

	caCert, err := pcert.Parse(caCertPEM)
	if err != nil {
		t.Fatal(err)
	}

	caKey, err := pcert.ParseKey(caKeyPEM)
	if err != nil {
		t.Fatal(err)
	}

	serverCertPEM, serverKeyPEM, err := pcert.Create(pcert.NewServerCertificate(serverName), caCert, caKey)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(testCertDir, "server.crt"), serverCertPEM, 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(testCertDir, "server.key"), serverKeyPEM, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		log.Fatalf("Could not connect to Docker: %s", err)
	}

	var network *dockertest.Network
	networks, err := pool.NetworksByName(networkName)
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) == 0 {
		network, err = pool.CreateNetwork(networkName)
		if err != nil {
			t.Fatal("failed to create network", err)
		}
	} else {
		network = &networks[0]
	}

	defer func() {
		if skipCleanup {
			return
		}
		err := pool.RemoveNetwork(network)
		if err != nil {
			t.Log("failed to cleanup network", err)
		}
	}()

	absCertDir, err := filepath.Abs(testCertDir)
	if err != nil {
		t.Fatal(err)
	}

	// pulls an image, creates a container based on it and runs it
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "dvob/http-server",
		Tag:        "v0.0.9",
		NetworkID:  network.Network.ID,
		Cmd: []string{
			"-addr=:443",
			"-tls",
			"-cert=/certs/server.crt",
			"-key=/certs/server.key",
		},
		Name:     "image-ca-injector-test",
		Hostname: serverName,
		Mounts: []string{
			absCertDir + ":/certs",
		},
	})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}
	defer func() {
		if skipCleanup {
			return
		}
		if err := pool.Purge(resource); err != nil {
			t.Logf("Could not purge resource: %s", err)
		}
	}()

	ip := resource.GetIPInNetwork(network)
	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig.InsecureSkipVerify = true
		c := &http.Client{
			Transport: tr,
		}
		u := fmt.Sprintf("https://%s", ip)
		_, err := c.Get(u)
		if err != nil {
			t.Log(err)
		}
		return err
	}); err != nil {
		t.Fatalf("Could not connect to https server: %s", err)
	}

	caCertFile := filepath.Join(testCertDir, "myca.crt")

	t.Run("alpine", func(t *testing.T) {
		pull(t, pool.Client, "alpine", "latest")

		err = injectCA(&opts{
			src:     "alpine",
			dst:     "myalpine",
			srcType: "docker",
			dstType: "docker",
			caFile:  caCertFile,
		})
		if err != nil {
			t.Fatal(err)
		}

		t.Run("base", func(t *testing.T) {
			cleanup := dockerRun(t, pool, &dockertest.RunOptions{
				Repository: "myalpine",
				NetworkID:  network.Network.ID,
				Cmd: []string{
					"wget",
					"https://test.example.com",
				},
				Name: "image-ca-injector-test-alpine-base",
			})
			t.Cleanup(cleanup)
		})
		t.Run("curl", func(t *testing.T) {
			cleanup := dockerRun(t, pool, &dockertest.RunOptions{
				Repository: "myalpine",
				NetworkID:  network.Network.ID,
				Cmd: []string{
					"sh",
					"-c",
					`
					set -o errexit
					apk add curl
					curl -v https://test.example.com
					`,
				},
				Name: "image-ca-injector-test-alpine-curl",
			})
			t.Cleanup(cleanup)
		})
	})
	t.Run("debian", func(t *testing.T) {
		pull(t, pool.Client, "debian", "latest")

		err = injectCA(&opts{
			src:     "debian",
			dst:     "mydebian",
			srcType: "docker",
			dstType: "docker",
			caFile:  caCertFile,
		})
		if err != nil {
			t.Fatal(err)
		}

		t.Run("curl", func(t *testing.T) {
			cleanup := dockerRun(t, pool, &dockertest.RunOptions{
				Repository: "mydebian",
				NetworkID:  network.Network.ID,
				Cmd: []string{
					"sh",
					"-c",
					`
					set -o errexit
					apt-get update
					apt-get install -y curl
					curl -v https://test.example.com
					`,
				},
				Name: "image-ca-injector-test-debian-curl",
			})
			t.Cleanup(cleanup)
		})
	})
	t.Run("ubuntu", func(t *testing.T) {
		pull(t, pool.Client, "ubuntu", "latest")

		err = injectCA(&opts{
			src:     "ubuntu",
			dst:     "myubuntu",
			srcType: "docker",
			dstType: "docker",
			caFile:  caCertFile,
		})
		if err != nil {
			t.Fatal(err)
		}

		t.Run("curl", func(t *testing.T) {
			cleanup := dockerRun(t, pool, &dockertest.RunOptions{
				Repository: "myubuntu",
				NetworkID:  network.Network.ID,
				Cmd: []string{
					"sh",
					"-c",
					`
					set -o errexit
					apt-get update
					apt-get install -y curl
					curl -v https://test.example.com
					`,
				},
				Name: "image-ca-injector-test-ubuntu-curl",
			})
			t.Cleanup(cleanup)
		})
	})
	for _, v := range []string{"8", "9"} {
		v := v
		t.Run("rockylinux:"+v, func(t *testing.T) {
			pull(t, pool.Client, "rockylinux", v)

			err = injectCA(&opts{
				src:     "rockylinux:" + v,
				dst:     "myrockylinux:" + v,
				srcType: "docker",
				dstType: "docker",
				caFile:  caCertFile,
			})
			if err != nil {
				t.Fatal(err)
			}

			t.Run("curl", func(t *testing.T) {
				cleanup := dockerRun(t, pool, &dockertest.RunOptions{
					Repository: "myrockylinux",
					Tag:        v,
					NetworkID:  network.Network.ID,
					Cmd: []string{
						"sh",
						"-c",
						`curl -v https://test.example.com
						`,
					},
					Name: "image-ca-injector-test-rocklinux" + v + "-curl",
				})
				t.Cleanup(cleanup)
			})
			t.Run("openssl", func(t *testing.T) {
				cleanup := dockerRun(t, pool, &dockertest.RunOptions{
					Repository: "myrockylinux",
					Tag:        v,
					NetworkID:  network.Network.ID,
					Cmd: []string{
						"sh",
						"-c",
						`yum install -y openssl
						echo '' | openssl s_client -connect test.example.com:443
						`,
					},
					Name: "image-ca-injector-test-rockylinux" + v + "-openssl",
				})
				t.Cleanup(cleanup)
			})
		})
	}
	t.Run("openjdk", func(t *testing.T) {
		t.Skip()
	})
}

func pull(t *testing.T, c *docker.Client, repository string, tag string) {
	t.Helper()
	t.Logf("pull %s:%s", repository, tag)
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Minute*5)
	t.Cleanup(cancelFn)
	err := c.PullImage(docker.PullImageOptions{
		Repository: "alpine",
		Tag:        "latest",
		Context:    ctx,
	}, docker.AuthConfiguration{})
	if err != nil {
		t.Fatal(err)
	}
}

func dockerRun(t *testing.T, p *dockertest.Pool, config *dockertest.RunOptions) func() {
	resource, err := p.RunWithOptions(config)
	cleanup := func() {
		if err := p.Purge(resource); err != nil {
			t.Logf("Could not purge container '%s': %s", config.Name, err)
		}
	}
	if err != nil {
		t.Fatalf("failed to run container %s: %s", config.Name, err)
		return cleanup
	}

	c := resource.Container
	exitCode, err := p.Client.WaitContainerWithContext(resource.Container.ID, context.Background())
	if err != nil {
		t.Fatalf("failed to wait on container '%s': %s", resource.Container.ID, err)
	}

	if exitCode == 0 {
		if _, ok := os.LookupEnv("SHOW_LOG"); ok {
			err = p.Client.Logs(docker.LogsOptions{
				Container:    c.ID,
				OutputStream: os.Stderr,
				ErrorStream:  os.Stderr,
				Stdout:       true,
				Stderr:       true,
			})
			if err != nil {
				t.Logf("failed to show log: %s", err)
			}
		}
		return cleanup
	}

	logOut := &bytes.Buffer{}
	err = p.Client.Logs(docker.LogsOptions{
		Container:    c.ID,
		OutputStream: logOut,
		ErrorStream:  logOut,
		Stdout:       true,
		Stderr:       true,
	})
	if err != nil {
		t.Fatalf("container '%s' exited with code %d. failed to get logs:", resource.Container.ID, err)
		return cleanup
	} else {
		t.Fatalf("container '%s' exited with code %d. output: '%s'", resource.Container.ID, exitCode, logOut.String())
		return cleanup
	}
}

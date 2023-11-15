package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/dvob/pcert"
	"github.com/ory/dockertest/v3"
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
		err := injectCA(&opts{
			src:     "alpine",
			dst:     "myalpine",
			srcType: "remote",
			dstType: "docker",
			caFile:  caCertFile,
		})
		if err != nil {
			t.Fatal(err)
		}
		resource, err := pool.RunWithOptions(&dockertest.RunOptions{
			Repository: "myalpine",
			NetworkID:  network.Network.ID,
			Cmd: []string{
				"wget",
				"https://test.example.com",
			},
			Name: "image-ca-injector-test-alpine",
		})
		if err != nil {
			log.Fatalf("Could not start resource: %s", err)
		}

		cleanup := func() {
			if skipCleanup {
				return
			}
			if err := pool.Purge(resource); err != nil {
				t.Logf("Could not purge resource: %s", err)
			}
		}

		defer cleanup()

		c := resource.Container
		if err := pool.Retry(func() error {
			r, _ := pool.ContainerByName(resource.Container.Name)
			if r == nil {
				return fmt.Errorf("could not get container with name %s", resource.Container.Name)
			}
			c = r.Container
			if c.State.Running {
				return fmt.Errorf("container still running")
			}
			return nil
		}); err != nil {
			t.Fatalf("container did not finish: %s", err)
		}
		if c.State.ExitCode != 0 {
			t.Fatal("command in alpine container was not successful")
		}
	})

	t.Run("openjdk", func(t *testing.T) {
		t.Skip()
	})
}

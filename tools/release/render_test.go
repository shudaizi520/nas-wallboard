package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func readProjectFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

const releaseTemplateFixture = `
services:
  volume-init:
    image: {{IMAGE}}
  wallboard:
    image: {{IMAGE}}
    environment:
      WALLBOARD_VERSION: {{VERSION}}
`

func TestRenderInstallYAMLUsesDigestPinnedImage(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	output, err := Render([]byte(releaseTemplateFixture), Options{
		Repository: "Example-Org/nas-wallboard",
		Version:    "v1.2.3-beta.1",
		Digest:     digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	image := "ghcr.io/example-org/nas-wallboard@" + digest
	if strings.Count(text, image) != 2 {
		t.Fatalf("rendered image count = %d\n%s", strings.Count(text, image), text)
	}
	for _, forbidden := range []string{"latest", "{{", "OWNER", ":v1.2.3-beta.1"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("rendered YAML contains %q\n%s", forbidden, text)
		}
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("rendered YAML is invalid: %v", err)
	}
}

func TestRenderInstallYAMLRejectsUntrustedInputs(t *testing.T) {
	valid := Options{Repository: "owner/nas-wallboard", Version: "v1.2.3", Digest: "sha256:" + strings.Repeat("b", 64)}
	for name, mutate := range map[string]func(*Options){
		"repository": func(options *Options) { options.Repository = "owner/repo/extra" },
		"version":    func(options *Options) { options.Version = "latest" },
		"digest":     func(options *Options) { options.Digest = "sha256:not-a-digest" },
	} {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if _, err := Render([]byte(releaseTemplateFixture), options); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
}

func TestProductionTemplateKeepsRuntimeConfined(t *testing.T) {
	template := readProjectFile(t, "deploy/nas-wallboard-truenas.yaml.tmpl")
	digest := "sha256:" + strings.Repeat("c", 64)
	output, err := Render(template, Options{Repository: "owner/nas-wallboard", Version: "v1.0.0", Digest: digest})
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, required := range []string{
		"user: \"65532:65532\"", "read_only: true", "cap_drop:", "- ALL",
		"no-new-privileges:true", "pids_limit: 64", "mem_limit: 128m", "cpus: \"0.50\"",
		"/data", "/tmp:rw,noexec,nosuid,nodev,size=16m", "condition: service_completed_successfully",
		"healthcheck", "restart: unless-stopped",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("release template missing %q", required)
		}
	}
	if strings.Count(text, "user: \"0:0\"") != 1 {
		t.Fatalf("only initializer may run as root:\n%s", text)
	}
}

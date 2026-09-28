package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image      string         `yaml:"image"`
	Build      any            `yaml:"build"`
	User       string         `yaml:"user"`
	ReadOnly   bool           `yaml:"read_only"`
	Privileged bool           `yaml:"privileged"`
	PID        string         `yaml:"pid"`
	Restart    string         `yaml:"restart"`
	EntryPoint []string       `yaml:"entrypoint"`
	Command    []string       `yaml:"command"`
	DependsOn  map[string]any `yaml:"depends_on"`
	Ports      []string       `yaml:"ports"`
	Volumes    []struct {
		Type     string `yaml:"type"`
		Source   string `yaml:"source"`
		Target   string `yaml:"target"`
		ReadOnly bool   `yaml:"read_only"`
	} `yaml:"volumes"`
	Tmpfs       []string `yaml:"tmpfs"`
	CapDrop     []string `yaml:"cap_drop"`
	CapAdd      []string `yaml:"cap_add"`
	SecurityOpt []string `yaml:"security_opt"`
	PidsLimit   int      `yaml:"pids_limit"`
	MemLimit    string   `yaml:"mem_limit"`
	CPUs        string   `yaml:"cpus"`
	Healthcheck struct {
		Test []string `yaml:"test"`
	} `yaml:"healthcheck"`
}

func projectFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func loadCompose(t *testing.T, name string) composeFile {
	t.Helper()
	var document composeFile
	if err := yaml.Unmarshal(projectFile(t, name), &document); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return document
}

func TestContainerContractDockerfile(t *testing.T) {
	dockerfile := string(projectFile(t, "Dockerfile"))
	for _, required := range []string{
		"FROM golang:1.27.1-alpine AS build", "CGO_ENABLED=0", "-trimpath", "-buildid=",
		"-X main.version=", "-X main.repository=", "FROM scratch", "USER 65532:65532",
		"ENTRYPOINT [\"/app/wallboard\"]",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile missing %q", required)
		}
	}
	for _, forbidden := range []string{"USER root", "mcr.microsoft.com/dotnet", ":latest"} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("Dockerfile contains %q", forbidden)
		}
	}
}

func TestLocalComposeMatchesRuntimeSecurityContract(t *testing.T) {
	document := loadCompose(t, "compose.yaml")
	wallboard, ok := document.Services["wallboard"]
	if !ok {
		t.Fatal("compose has no wallboard service")
	}
	initializer, ok := document.Services["volume-init"]
	if !ok {
		t.Fatal("compose has no volume initializer")
	}
	if wallboard.User != "65532:65532" || !wallboard.ReadOnly || wallboard.Privileged || wallboard.PID == "host" {
		t.Fatalf("unsafe runtime flags: %#v", wallboard)
	}
	if wallboard.Restart != "unless-stopped" || len(wallboard.DependsOn) != 1 {
		t.Fatalf("runtime lifecycle = restart %q depends_on %#v", wallboard.Restart, wallboard.DependsOn)
	}
	if len(wallboard.Ports) != 1 || !strings.HasSuffix(wallboard.Ports[0], ":8080") {
		t.Fatalf("published ports = %#v", wallboard.Ports)
	}
	if len(wallboard.Volumes) != 1 || wallboard.Volumes[0].Target != "/data" || wallboard.Volumes[0].ReadOnly {
		t.Fatalf("runtime data volume = %#v", wallboard.Volumes)
	}
	if !equalStrings(wallboard.CapDrop, []string{"ALL"}) || !contains(wallboard.SecurityOpt, "no-new-privileges:true") {
		t.Fatalf("runtime security = %#v / %#v", wallboard.CapDrop, wallboard.SecurityOpt)
	}
	if !containsPart(wallboard.Tmpfs, "/tmp") || !containsPart(wallboard.Tmpfs, "noexec") || wallboard.PidsLimit != 64 || wallboard.MemLimit != "128m" || wallboard.CPUs != "0.50" {
		t.Fatalf("runtime limits = %#v", wallboard)
	}
	if !contains(wallboard.Healthcheck.Test, "healthcheck") {
		t.Fatalf("runtime healthcheck = %#v", wallboard.Healthcheck.Test)
	}
	if initializer.User != "0:0" || !initializer.ReadOnly || initializer.Restart != "no" || !contains(initializer.Command, "init-data") {
		t.Fatalf("initializer contract = %#v", initializer)
	}
	if !equalStrings(initializer.CapAdd, []string{"CHOWN", "FOWNER"}) || initializer.PidsLimit > 16 || initializer.MemLimit != "32m" {
		t.Fatalf("initializer privileges/limits = %#v", initializer)
	}
}

func TestPublicReleasePathsContainNoOwnerSpecificAddressOrPlaceholder(t *testing.T) {
	paths := []string{"Dockerfile", "compose.yaml", "README.md", "docs/truenas-install.md", "docs/windows-desktop-widget.md", "windows/NASWallboard.Desktop.Core/AppSettings.cs"}
	for _, path := range paths {
		contents := string(projectFile(t, path))
		for _, forbidden := range []string{strings.Join([]string{"192", "168", "50", "99"}, "."), "REPLACE_WITH", "ghcr.io/OWNER", "test-secret"} {
			if strings.Contains(contents, forbidden) {
				t.Errorf("%s contains %q", path, forbidden)
			}
		}
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsPart(values []string, wanted string) bool {
	for _, value := range values {
		if strings.Contains(value, wanted) {
			return true
		}
	}
	return false
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

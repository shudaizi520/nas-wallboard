package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type releaseStep struct {
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

func clientReleaseSteps(t *testing.T) []releaseStep {
	t.Helper()
	body, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []releaseStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	var selected []releaseStep
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Name == "Build Windows client once" || step.Name == "Reject non-semantic or unsafe release tags before publishing" {
				selected = append(selected, step)
			}
		}
	}
	if len(selected) != 2 {
		t.Fatal("missing client publish or tag validation step")
	}
	return selected
}

func runClientReleaseStep(t *testing.T, step releaseStep, tag, root string) (string, error) {
	t.Helper()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	// Only the expensive .NET compiler is replaced. Bash argument parsing and the
	// real release validator still run, which are the boundaries under test.
	if err := os.WriteFile(filepath.Join(bin, "dotnet"), []byte("#!/bin/sh\nprintf 'CLIENT_PUBLISH\\n'\nprintf '%s\\n' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(step.Run, "${{ github.ref_name }}", tag)
	script = strings.ReplaceAll(script, "/tmp/nas-wallboard-release-validation.yaml", filepath.Join(root, "validated.yaml"))
	command := exec.Command("bash", "-c", script)
	command.Dir = ".."
	command.Env = append(os.Environ(), "GITHUB_REF_NAME="+tag, "GITHUB_REPOSITORY=owner/repo", "PATH="+bin+string(os.PathListSeparator)+filepath.Join(runtime.GOROOT(), "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestClientPublishTreatsTagAsOneLiteralArgument(t *testing.T) {
	for _, step := range clientReleaseSteps(t) {
		if step.Name != "Build Windows client once" {
			continue
		}
		output, err := runClientReleaseStep(t, step, "v1.0.10;true", t.TempDir())
		if err != nil || !strings.Contains(output, "-p:WallboardReleaseVersion=v1.0.10;true\n") {
			t.Fatalf("tag was interpreted by shell instead of passed as one argument: %v %q", err, output)
		}
	}
}

func TestUnsafeReleaseTagIsRejectedBeforeAnyClientPublish(t *testing.T) {
	root := t.TempDir()
	for _, step := range clientReleaseSteps(t) {
		output, err := runClientReleaseStep(t, step, "v1.0.10;true", root)
		if strings.Contains(output, "CLIENT_PUBLISH") {
			t.Fatal("unsafe release reached the compiler before validation")
		}
		if err != nil {
			return
		}
	}
	t.Fatal("unsafe release tag was not rejected")
}

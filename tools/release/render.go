package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_.-]{0,38})/[A-Za-z0-9_.-]+$`)
	versionPattern    = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$`)
	digestPattern     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type Options struct {
	Repository string
	Version    string
	Digest     string
}

func Render(template []byte, options Options) ([]byte, error) {
	if !repositoryPattern.MatchString(options.Repository) {
		return nil, errors.New("repository must be owner/name")
	}
	if !versionPattern.MatchString(options.Version) {
		return nil, errors.New("version must be a semantic version tag beginning with v")
	}
	if !digestPattern.MatchString(options.Digest) {
		return nil, errors.New("digest must be a sha256 OCI digest")
	}
	image := "ghcr.io/" + strings.ToLower(options.Repository) + "@" + options.Digest
	replacer := strings.NewReplacer("{{IMAGE}}", image, "{{VERSION}}", options.Version)
	output := []byte(replacer.Replace(string(template)))
	if bytes.Contains(output, []byte("{{")) || bytes.Contains(output, []byte("}}")) {
		return nil, errors.New("template contains unresolved placeholders")
	}
	return output, nil
}

func main() {
	templatePath := flag.String("template", "deploy/nas-wallboard-truenas.yaml.tmpl", "input template")
	outputPath := flag.String("output", "nas-wallboard-truenas.yaml", "output path")
	repository := flag.String("repository", "", "GitHub owner/name")
	version := flag.String("version", "", "release tag")
	digest := flag.String("digest", "", "published image digest")
	flag.Parse()

	template, err := os.ReadFile(*templatePath)
	if err != nil {
		fatal(err)
	}
	output, err := Render(template, Options{Repository: *repository, Version: *version, Digest: *digest})
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*outputPath, output, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

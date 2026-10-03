package desktop

import (
	"io"
	"io/fs"
	"regexp"
	"strings"
)

var releaseVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

func NormalizeVersion(value string) string {
	if len(value) > 96 || !releaseVersionPattern.MatchString(value) {
		return ""
	}
	tag, _, _ := strings.Cut(value, "+")
	return "v" + strings.TrimPrefix(tag, "v")
}

// PackagedVersion identifies the executable actually embedded with this server.
// It must not infer a client release from the server's own build version.
func PackagedVersion(assets fs.FS) string {
	if assets == nil {
		return ""
	}
	file, err := assets.Open("downloads/desktop-version.txt")
	if err != nil {
		return ""
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 128))
	if err != nil || len(body) >= 128 {
		return ""
	}
	return NormalizeVersion(strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff")))
}

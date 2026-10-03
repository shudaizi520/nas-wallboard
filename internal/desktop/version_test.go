package desktop

import (
	"testing"
	"testing/fstest"
)

func TestPackagedVersionReadsTheBundledClientInsteadOfGuessingServerVersion(t *testing.T) {
	for _, tt := range []struct{ name, content, want string }{
		{"release", "v1.0.10\n", "v1.0.10"},
		{"commit metadata", "1.0.10+abc123\n", "v1.0.10"},
		{"MSBuild UTF8 manifest", "\ufeffv1.0.10+6c004513faeae7ebed194a1a498de5a20b98038e\n", "v1.0.10"},
		{"prerelease", "v1.0.10-beta.2+abc123\n", "v1.0.10-beta.2"},
		{"local build", "unknown\n", ""},
		{"unsafe value", "v1.0.10<script>", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assets := fstest.MapFS{"downloads/desktop-version.txt": &fstest.MapFile{Data: []byte(tt.content)}}
			if got := PackagedVersion(assets); got != tt.want {
				t.Fatalf("packaged version = %q, want %q", got, tt.want)
			}
		})
	}
	if got := PackagedVersion(fstest.MapFS{}); got != "" {
		t.Fatalf("missing manifest guessed %q", got)
	}
}

func TestNormalizeVersionRejectsUnsafeOrAmbiguousVersions(t *testing.T) {
	for _, input := range []string{"", "unknown", "dev+hash", "v01.0.10", "v1.0", "v1.0.10<script>", " v1.0.10 "} {
		if got := NormalizeVersion(input); got != "" {
			t.Fatalf("unsafe %q normalized to %q", input, got)
		}
	}
}

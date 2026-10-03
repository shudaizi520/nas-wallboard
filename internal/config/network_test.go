package config

import "testing"

func TestLoadRejectsUnsafeNetworkInterface(t *testing.T) {
	for _, identifier := range []string{"../eth0", "eth0/other"} {
		path := minimalConfig(t, "dashboard:\n  metrics:\n    - type: network\n      interface: "+identifier+"\n")
		if _, err := Load(path, safeSecretFile(t, testSecret)); err == nil {
			t.Fatalf("unsafe YAML interface accepted: %q", identifier)
		}
	}
}

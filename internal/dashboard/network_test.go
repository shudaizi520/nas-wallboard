package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
	"go.yaml.in/yaml/v3"
)

// Ignoring the configured identifier would show auto rates for a missing NIC.
func TestNetworkMetricSelectedInterfaceNeverFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name, selected, want string
		stale                bool
	}{
		{"explicit", "eth0", "↓ 125 KB/s ↑ 250 KB/s", false},
		{"missing", "gone0", "—", false},
		{"incomplete", "br0", "—", false},
		{"true zero", "lo", "↓ 0 B/s ↑ 0 B/s", false},
		{"stale", "eth0", "—", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg config.DashboardConfig
			if err := yaml.Unmarshal([]byte("metrics:\n  - type: network\n    interface: "+tc.selected+"\n"), &cfg); err != nil {
				t.Fatal(err)
			}
			builder, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var realtime model.RealtimeStatus
			if err := json.Unmarshal([]byte(`{"network_rx_bps":9000000,"network_tx_bps":8000000,"network_interfaces":[{"identifier":"eth0","rx_bps":125000,"tx_bps":250000,"available":true},{"identifier":"br0","available":false},{"identifier":"lo","rx_bps":0,"tx_bps":0,"available":true}]}`), &realtime); err != nil {
				t.Fatal(err)
			}
			got := builder.Build(model.Snapshot{Realtime: model.Module[model.RealtimeStatus]{Data: realtime, Stale: tc.stale}}, time.Now()).Metrics[0]
			if got.Value != tc.want {
				t.Fatalf("metric = %#v, want %q", got, tc.want)
			}
			if (got.Tone == "bad") != (tc.want == "—") {
				t.Fatalf("tone = %s", got.Tone)
			}
		})
	}
}

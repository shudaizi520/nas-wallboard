package truenas

import (
	"context"
	"encoding/json"
	"testing"
)

// Independent maxima would combine eth0's receive rate with br0's send rate.
func TestRealtimeAutoNetworkUsesOneInterface(t *testing.T) {
	for _, rows := range []string{
		`{"name":"interface","identifier":"eth0","legend":["received","sent"],"data":[[8000,1000]]},{"name":"interface","identifier":"br0","legend":["received","sent"],"data":[[4000,6000]]}`,
		`{"name":"interface","identifier":"br0","legend":["received","sent"],"data":[[4000,6000]]},{"name":"interface","identifier":"eth0","legend":["received","sent"],"data":[[8000,1000]]}`,
	} {
		caller := networkCaller(rows)
		got, err := NewCollectors(caller, nil).CollectRealtime(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got.NetworkRxBps != 500000 || got.NetworkTxBps != 750000 {
			t.Fatalf("auto rates = %v/%v, want br0 pair 500000/750000", got.NetworkRxBps, got.NetworkTxBps)
		}
	}
}

func TestRealtimeAutoNetworkTieUsesIdentifier(t *testing.T) {
	caller := networkCaller(`{"name":"interface","identifier":"eth0","legend":["received","sent"],"data":[[8000,2000]]},{"name":"interface","identifier":"br0","legend":["received","sent"],"data":[[4000,6000]]}`)
	got, err := NewCollectors(caller, nil).CollectRealtime(context.Background())
	if err != nil || got.NetworkRxBps != 500000 || got.NetworkTxBps != 750000 {
		t.Fatalf("tie = %#v, %v; want lexicographic br0 pair", got, err)
	}
}

func TestRealtimeDoesNotCombineIncompleteNetworkSamples(t *testing.T) {
	for _, rows := range []string{
		`{"name":"interface","identifier":"eth0","legend":["received","sent"],"data":[[8000,null]]},{"name":"interface","identifier":"br0","legend":["received","sent"],"data":[[null,6000]]}`,
		`{"name":"interface","identifier":"eth0","legend":["received","sent"],"data":[[8000,null],[null,6000]]}`,
	} {
		got, err := NewCollectors(networkCaller(rows), nil).CollectRealtime(context.Background())
		if err == nil {
			t.Fatalf("incomplete samples reported as successful: %#v", got)
		}
	}
}

func networkCaller(rows string) *fixtureCaller {
	return &fixtureCaller{responses: map[string][]json.RawMessage{
		"reporting.netdata_graphs":   {json.RawMessage(`[{"name":"interface","identifiers":["eth0","br0"]}]`)},
		"system.info":                {json.RawMessage(`{"physmem":100}`)},
		"reporting.netdata_get_data": {json.RawMessage(`[{"name":"cpu","legend":["cpu"],"data":[[42]]},{"name":"memory","legend":["available"],"data":[[20]]},` + rows + `]`)},
	}}
}

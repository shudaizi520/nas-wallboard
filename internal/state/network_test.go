package state

import (
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestNetworkInterfaceSamplesAreIsolatedFromCallerAndSnapshot(t *testing.T) {
	now := time.Unix(100, 0)
	store := testStore(&now)
	value := model.RealtimeStatus{NetworkInterfaces: []model.NetworkInterfaceStatus{{Identifier: "eth0", RxBps: 125000, TxBps: 250000, Available: true}}}
	store.SetRealtime(value, nil)
	value.NetworkInterfaces[0].Identifier = "caller-mutated"
	first := store.Snapshot()
	if first.Realtime.Data.NetworkInterfaces[0].Identifier != "eth0" {
		t.Fatal("caller mutated stored data")
	}
	first.Realtime.Data.NetworkInterfaces[0].Identifier = "snapshot-mutated"
	if store.Snapshot().Realtime.Data.NetworkInterfaces[0].Identifier != "eth0" {
		t.Fatal("snapshot mutated stored data")
	}
}

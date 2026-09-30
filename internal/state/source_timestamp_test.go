package state

import (
	"errors"
	"example.com/nas-wallboard/internal/model"
	"testing"
	"time"
)

func TestSharedReadsKeepOriginalSourceTimestamp(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	store := testStore(&now)
	stamp := now.Add(-time.Minute)
	store.SetSystemAt(model.SystemStatus{Hostname: "NAS"}, nil, stamp)
	store.SetMemoryAt(model.MemoryStatus{AvailablePercent: 50}, nil, stamp)
	store.SetAlertsAt([]model.AlertStatus{{ID: "1"}}, nil, stamp)
	store.SetTrueNASDiskHealthAt([]model.DiskHealthStatus{{Name: "sda", State: "failed"}}, nil, stamp)
	got := store.Snapshot()
	if !got.System.UpdatedAt.Equal(stamp) || !got.Memory.UpdatedAt.Equal(stamp) || !got.Alerts.UpdatedAt.Equal(stamp) || !got.TrueNASDiskHealth.UpdatedAt.Equal(stamp) {
		t.Fatal("cache publication renewed source freshness")
	}
	store.SetSystemAt(model.SystemStatus{Hostname: "wrong"}, errors.New("failed"), now)
	if !store.Snapshot().System.UpdatedAt.Equal(stamp) || store.Snapshot().System.Data.Hostname != "NAS" {
		t.Fatal("failed read replaced original timestamp or data")
	}
}

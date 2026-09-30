package state

import (
	"errors"
	"example.com/nas-wallboard/internal/model"
	"testing"
	"time"
)

func TestDiskHealthSourcesDoNotEraseEachOther(t *testing.T) {
	now := time.Unix(100, 0)
	s := testStore(&now)
	s.SetDiskHealth([]model.DiskHealthStatus{{Name: "sda", Model: "MODEL", SizeBytes: 1000, State: "failed"}}, nil)
	s.SetTrueNASDiskHealth(nil, nil)
	if got := s.Snapshot().DiskHealth; len(got.Data) != 1 || got.Data[0].State != "failed" {
		t.Fatalf("Scrutiny data erased: %+v", got)
	}
	s.SetTrueNASDiskHealth([]model.DiskHealthStatus{{Name: "SMART 告警", State: "failed"}}, nil)
	s.SetDiskHealth([]model.DiskHealthStatus{{Name: "sda", State: "healthy"}}, nil)
	if got := s.Snapshot().DiskHealth; len(got.Data) != 2 {
		t.Fatalf("TrueNAS alert erased: %+v", got)
	}
	now = now.Add(7 * time.Minute)
	s.SetTrueNASDiskHealth(nil, nil)
	if s.Snapshot().DiskHealth.Stale {
		t.Fatal("Scrutiny expired before scheduled 30-minute refresh")
	}
	s.SetTrueNASDiskHealth(nil, errors.New("unavailable"))
	if s.Snapshot().DiskHealth.Error == "" {
		t.Fatal("partial source failure hidden")
	}
}

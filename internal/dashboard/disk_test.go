package dashboard

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
)

func TestSelectDiskUsesModelAndSizeAcrossDeviceRename(t *testing.T) {
	disks := []model.DiskStatus{
		{ID: "nvme0n1", Name: "nvme0n1", Model: "E2M2 64GB", SizeBytes: 61865982976, Temperature: 48},
		{ID: "sdd", Name: "sdd", Model: "ST14000NM001G-2KJ103", SizeBytes: 14000519643136, Temperature: 40},
	}
	match := config.DiskMatchConfig{Model: "ST14000NM001G-2KJ103", SizeBytes: 14000519643136}

	got, err := SelectDisk(disks, match)
	if err != nil {
		t.Fatalf("SelectDisk() error = %v", err)
	}
	if got.ID != "sdd" || got.Temperature != 40 {
		t.Fatalf("SelectDisk() = %#v", got)
	}
}

func TestSelectDiskRejectsAmbiguousMatch(t *testing.T) {
	disks := []model.DiskStatus{
		{ID: "sda", Model: "same", SizeBytes: 100},
		{ID: "sdb", Model: "same", SizeBytes: 100},
	}
	_, err := SelectDisk(disks, config.DiskMatchConfig{Model: "same", SizeBytes: 100})
	if !errors.Is(err, ErrAmbiguousDisk) {
		t.Fatalf("SelectDisk() error = %v, want ErrAmbiguousDisk", err)
	}

	_, err = SelectDisk(disks, config.DiskMatchConfig{Name: "missing"})
	if !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("missing SelectDisk() error = %v, want ErrDiskNotFound", err)
	}
}

func TestDiskIdentityIsNotSerialized(t *testing.T) {
	disk := model.DiskStatus{
		ID: "sda", Name: "14T HDD", Temperature: 40,
		Model: "private-model", Serial: "private-serial", SizeBytes: 14000519643136,
	}
	encoded, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"private-model", "private-serial", "14000519643136"} {
		if strings.Contains(text, secret) {
			t.Fatalf("serialized disk identity %q: %s", secret, text)
		}
	}
}

package dashboard

import (
	"errors"
	"fmt"
	"strings"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/model"
)

var (
	ErrDiskNotFound  = errors.New("configured disk was not found")
	ErrAmbiguousDisk = errors.New("configured disk match is ambiguous")
)

func SelectDisk(disks []model.DiskStatus, match config.DiskMatchConfig) (model.DiskStatus, error) {
	serial := strings.TrimSpace(match.Serial)
	modelName := strings.TrimSpace(match.Model)
	deviceName := strings.TrimSpace(match.Name)

	matches := make([]model.DiskStatus, 0, 1)
	for _, disk := range disks {
		selected := false
		switch {
		case serial != "":
			selected = disk.Serial == serial
		case modelName != "" && match.SizeBytes > 0:
			selected = disk.Model == modelName && disk.SizeBytes == match.SizeBytes
		case deviceName != "":
			selected = disk.ID == deviceName || disk.Name == deviceName
		}
		if selected {
			matches = append(matches, disk)
		}
	}

	switch len(matches) {
	case 0:
		return model.DiskStatus{}, ErrDiskNotFound
	case 1:
		return matches[0], nil
	default:
		return model.DiskStatus{}, fmt.Errorf("%w: %d disks matched", ErrAmbiguousDisk, len(matches))
	}
}

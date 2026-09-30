package collector

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHealthTracksFailedRequestsAndLastSuccessfulCollection(t *testing.T) {
	r := NewRunner([]Job{{Name: "test", Every: time.Minute, Timeout: time.Second}})
	if good, _, stamp := r.CollectionHealth(); good || !stamp.IsZero() {
		t.Fatal("uncollected job reported healthy")
	}
	r.recordResult("test", nil)
	good, _, stamp := r.CollectionHealth()
	if !good || stamp.IsZero() {
		t.Fatal("successful collection not recorded")
	}
	r.recordResult("test", ErrSkipped)
	if _, _, got := r.CollectionHealth(); !got.Equal(stamp) {
		t.Fatal("cached poll changed collection time")
	}
	r.recordResult("test", errors.New("private upstream token"))
	if good, message, got := r.CollectionHealth(); good || message != "采集失败" || !got.Equal(stamp) {
		t.Fatal("failure lost or success timestamp replaced")
	}
	r.recordResult("test", context.DeadlineExceeded)
	if good, _, _ := r.CollectionHealth(); good {
		t.Fatal("timeout reported healthy")
	}
}

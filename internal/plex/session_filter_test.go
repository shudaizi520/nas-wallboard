package plex

import (
	"testing"
	"time"

	"example.com/nas-wallboard/internal/model"
)

func TestSessionFilterHidesPlayingSessionAfterProgressStalls(t *testing.T) {
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	filter := NewSessionFilter(func() time.Time { return now })
	session := model.MediaSession{SessionID: "living-room", Title: "电影", Device: "电视", ProgressMillis: 30_000}

	if got := filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}}); len(got.Sessions) != 1 {
		t.Fatalf("new playing session was hidden: %#v", got)
	}
	now = now.Add(119 * time.Second)
	if got := filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}}); len(got.Sessions) != 1 {
		t.Fatalf("playing session expired too early: %#v", got)
	}
	now = now.Add(time.Second)
	if got := filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}}); len(got.Sessions) != 0 {
		t.Fatalf("stalled playing session remained visible: %#v", got)
	}
}

func TestSessionFilterHidesPausedSessionAfterOneMinute(t *testing.T) {
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	filter := NewSessionFilter(func() time.Time { return now })
	session := model.MediaSession{SessionID: "bedroom", Title: "儿歌", Device: "平板", Paused: true, ProgressMillis: 15_000}

	if got := filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}}); len(got.Sessions) != 1 {
		t.Fatalf("new paused session was hidden: %#v", got)
	}
	now = now.Add(59 * time.Second)
	if got := filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}}); len(got.Sessions) != 1 {
		t.Fatalf("paused session expired too early: %#v", got)
	}
	now = now.Add(time.Second)
	if got := filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}}); len(got.Sessions) != 0 {
		t.Fatalf("long-paused session remained visible: %#v", got)
	}
}

func TestSessionFilterTreatsReappearingSessionAsFresh(t *testing.T) {
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	filter := NewSessionFilter(func() time.Time { return now })
	session := model.MediaSession{SessionID: "web", Title: "音乐", Device: "浏览器", ProgressMillis: 8_000}

	filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}})
	now = now.Add(2 * time.Minute)
	filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}})
	filter.Apply(model.MediaStatus{})
	now = now.Add(time.Minute)
	if got := filter.Apply(model.MediaStatus{Sessions: []model.MediaSession{session}}); len(got.Sessions) != 1 {
		t.Fatalf("reappearing session inherited stale expiry: %#v", got)
	}
}

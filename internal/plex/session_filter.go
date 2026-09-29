package plex

import (
	"strings"
	"time"

	"example.com/nas-wallboard/internal/model"
)

const (
	playingStallTimeout = 2 * time.Minute
	pausedTimeout       = time.Minute
)

type sessionObservation struct {
	title          string
	progressMillis int64
	progressAt     time.Time
	pausedAt       time.Time
}

type SessionFilter struct {
	now      func() time.Time
	observed map[string]sessionObservation
}

func NewSessionFilter(now func() time.Time) *SessionFilter {
	return &SessionFilter{now: now, observed: map[string]sessionObservation{}}
}

func (filter *SessionFilter) Apply(status model.MediaStatus) model.MediaStatus {
	now := filter.now()
	visible := make([]model.MediaSession, 0, len(status.Sessions))
	seen := make(map[string]struct{}, len(status.Sessions))
	for _, session := range status.Sessions {
		key := sessionKey(session)
		seen[key] = struct{}{}
		observation, exists := filter.observed[key]
		if !exists || observation.title != session.Title {
			observation = sessionObservation{title: session.Title, progressMillis: session.ProgressMillis, progressAt: now}
		}

		expired := false
		if session.Paused {
			if observation.pausedAt.IsZero() {
				observation.pausedAt = now
			}
			expired = now.Sub(observation.pausedAt) >= pausedTimeout
		} else {
			observation.pausedAt = time.Time{}
			if observation.progressMillis != session.ProgressMillis {
				observation.progressMillis = session.ProgressMillis
				observation.progressAt = now
			}
			expired = now.Sub(observation.progressAt) >= playingStallTimeout
		}
		filter.observed[key] = observation
		if !expired {
			visible = append(visible, session)
		}
	}
	for key := range filter.observed {
		if _, exists := seen[key]; !exists {
			delete(filter.observed, key)
		}
	}
	status.Sessions = visible
	return status
}

func sessionKey(session model.MediaSession) string {
	if session.SessionID != "" {
		return session.SessionID
	}
	return strings.Join([]string{session.Title, session.Device, session.User}, "\x00")
}

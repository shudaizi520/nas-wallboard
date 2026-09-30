package integration

import (
	"context"
	"errors"
	"example.com/nas-wallboard/internal/collector"
	"example.com/nas-wallboard/internal/persist"
	"testing"
	"time"
)

func TestManagerReportsActualPollFailureWhileRunnerRemainsAlive(t *testing.T) {
	manager, _, _ := managerFixture(t, func(Config, Secrets) (Collector, error) {
		return collector.NewRunner([]collector.Job{{Name: "upstream", Every: time.Minute, Timeout: time.Second, Run: func(context.Context) error { return errors.New("private upstream credential") }}}), nil
	})
	defer manager.Close()
	if err := manager.Apply(context.Background(), nil, []persist.Integration{{ID: "one", Type: "plex", Enabled: true, Config: map[string]any{"url": "http://plex.local"}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		health := manager.Health()
		if len(health) == 1 && health[0].Message == "采集失败" {
			if health[0].Healthy || !health[0].Running || !health[0].LastSuccess.IsZero() {
				t.Fatal("incorrect failure health")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("running collector did not expose poll failure")
}

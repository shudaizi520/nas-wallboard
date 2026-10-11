package weather

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCachedAlertExpiresAtItsOwnBoundaryWithoutAnUpstreamRequest(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	current := `{"condition":{"text":"晴","code":"100"},"temperature":{"value":30}}`
	alerts := `{"alerts":[{"id":"ending","headline":"大风橙色预警","color":{"code":"orange"},"expireTime":"2026-10-11T12:01:00Z"},{"id":"ongoing","headline":"雷电黄色预警","color":{"code":"yellow"},"expireTime":"2026-10-11T13:00:00Z"}]}`
	c := validationClient(t, &now, &current, &alerts)
	first, err := c.Refresh(context.Background())
	if err != nil || len(first.Warnings) != 2 {
		t.Fatalf("valid warnings missing: %+v %v", first.Warnings, err)
	}
	alerts = `{}` // A request now would fail, proving this remains a cached read.
	now = now.Add(time.Minute)
	got, err := c.Refresh(context.Background())
	if err != nil || len(got.Warnings) != 1 || got.Warnings[0].ID != "ongoing" || !got.Components.Alerts.UpdatedAt.Equal(first.Components.Alerts.UpdatedAt) || !got.Components.Alerts.AttemptedAt.Equal(first.Components.Alerts.AttemptedAt) {
		t.Fatalf("expired alert retained or refresh performed: warnings=%+v metadata=%+v error=%v", got.Warnings, got.Components.Alerts, err)
	}
	a := validationActivity(t, got, now)
	if strings.Contains(a.Detail, "大风") || a.DetailParts == nil || !strings.Contains(a.Detail, "雷电黄色预警") {
		t.Fatalf("expired warning still displayed or ongoing alert lost: %+v", a)
	}
	got.Warnings[0].Title = "mutated"
	if next, _ := c.Refresh(context.Background()); next.Warnings[0].Title != "雷电黄色预警" || len(first.Warnings) != 2 {
		t.Fatal("projected warnings alias the source cache or previous output")
	}
}

func TestCancellationSupersedesOnlyItsReferencedAlertInEitherOrder(t *testing.T) {
	for _, items := range []string{
		`{"id":"old","headline":"大风橙色预警","color":{"code":"orange"}},{"id":"cancel","headline":"Wind warning cancelled","messageType":{"code":"cancel","supersedes":["old"]}},`,
		`{"id":"cancel","headline":"大风橙色预警取消","messageType":{"code":"cancel","supersedes":["old"]}},{"id":"old","headline":"大风橙色预警","color":{"code":"orange"}},`,
	} {
		now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		current := `{"condition":{"text":"晴","code":"100"},"temperature":{"value":30}}`
		alerts := `{"alerts":[` + items + `{"id":"other","headline":"暴雨红色预警","color":{"code":"red"}}]}`
		got, err := validationClient(t, &now, &current, &alerts).Refresh(context.Background())
		if err != nil || len(got.Warnings) != 1 || got.Warnings[0].ID != "other" {
			t.Fatalf("cancellation ignored or unrelated alert lost: %+v %v", got.Warnings, err)
		}
		a := validationActivity(t, got, now)
		if a.Detail != "暴雨红色预警" || a.DetailParts == nil {
			t.Fatalf("cancellation still rendered as danger: %+v", a)
		}
	}
}

func TestCancelWithoutSupersedesDoesNotCancelUnrelatedAlerts(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	current := `{"condition":{"text":"晴","code":"100"},"temperature":{"value":30}}`
	alerts := `{"alerts":[{"id":"cancel","headline":"大风预警取消","messageType":{"code":"cancel"}},{"id":"active","headline":"台风红色预警","color":{"code":"red"}}]}`
	got, err := validationClient(t, &now, &current, &alerts).Refresh(context.Background())
	if err != nil || len(got.Warnings) != 1 || got.Warnings[0].ID != "active" {
		t.Fatalf("cancel message displayed or unrelated warning cancelled: %+v %v", got.Warnings, err)
	}
}

func TestUpdatedAlertReplacesOldVersionWithoutRevivingExpiredReplacement(t *testing.T) {
	for _, expires := range []string{"2026-10-11T13:00:00Z", "2026-10-11T12:00:00Z"} {
		now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		current := `{"condition":{"text":"晴","code":"100"},"temperature":{"value":30}}`
		alerts := `{"alerts":[{"id":"old","headline":"台风红色预警","color":{"code":"red"}},{"id":"new","headline":"台风黄色预警","color":{"code":"yellow"},"messageType":{"code":"update","supersedes":["old"]},"expireTime":"` + expires + `"}]}`
		got, err := validationClient(t, &now, &current, &alerts).Refresh(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if expires == "2026-10-11T13:00:00Z" {
			if len(got.Warnings) != 1 || got.Warnings[0].ID != "new" || validationActivity(t, got, now).Detail != "台风黄色预警" {
				t.Fatalf("new severity failed to supersede old warning: %+v", got.Warnings)
			}
		} else if len(got.Warnings) != 0 {
			t.Fatalf("expired update revived old severity: %+v", got.Warnings)
		}
	}
}

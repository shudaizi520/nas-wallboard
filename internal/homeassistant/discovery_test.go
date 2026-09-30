package homeassistant

import (
	"context"
	"example.com/nas-wallboard/internal/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEntitiesListsOnlyFansAndPowerSensors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/states" || r.Header.Get("Authorization") != "Bearer private" {
			t.Error("incorrect discovery request")
		}
		_, _ = w.Write([]byte(`[{"entity_id":"fan.room","attributes":{"friendly_name":"客厅风扇"}},{"entity_id":"sensor.nas_power","attributes":{"unit_of_measurement":"W","friendly_name":"NAS功耗"}},{"entity_id":"sensor.temperature","attributes":{"unit_of_measurement":"°C"}},{"entity_id":"switch.plug"}]`))
	}))
	defer upstream.Close()
	list, err := New(config.HomeAssistantConfig{URL: upstream.URL}, "private", upstream.Client()).Entities(context.Background())
	if err != nil || len(list) != 2 || list[0].ID != "fan.room" || list[1].Kind != "power" {
		t.Fatalf("entities %#v / %v", list, err)
	}
}

package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"example.com/nas-wallboard/internal/model"
	"example.com/nas-wallboard/internal/widget"
)

func TestLayoutIncludesCollectedInterfaceChoicesWithoutNASQuery(t *testing.T) {
	_, configStore, _ := layoutFixture(t)
	registry, err := widget.BuiltInRegistry()
	if err != nil {
		t.Fatal(err)
	}
	runtimeStore := apiTestStore()
	runtimeStore.SetRealtime(model.RealtimeStatus{NetworkInterfaces: []model.NetworkInterfaceStatus{{Identifier: "eth0", Available: true}, {Identifier: "br0", Available: false}}}, nil)
	s := &server{store: runtimeStore, configState: configStore, widgets: widget.NewService(registry, configStore)}
	response := httptest.NewRecorder()
	s.writeLayout(response)
	var body struct {
		Interfaces []model.NetworkInterfaceStatus `json:"network_interfaces"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Interfaces) != 2 || body.Interfaces[0].Identifier != "eth0" || body.Interfaces[1].Available {
		t.Fatalf("choices = %#v", body.Interfaces)
	}
}

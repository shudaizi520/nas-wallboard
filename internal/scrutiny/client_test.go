package scrutiny

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
)

func TestProbeClassifiesInvalidServiceResponse(t *testing.T) {
	client, err := New(config.ScrutinyConfig{URL: "http://scrutiny:8080"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusBadGateway, "bad"), nil })})
	if err != nil {
		t.Fatal(err)
	}
	if result := client.Probe(context.Background()); result.OK || result.Stage != integration.ProbeStageFeature {
		t.Fatalf("Probe = %#v", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestSummaryNormalizesDeviceHealth(t *testing.T) {
	client, err := New(config.ScrutinyConfig{Enabled: true, URL: "http://scrutiny:8080"}, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "http://scrutiny:8080/api/summary" {
			t.Errorf("URL = %s", request.URL)
		}
		return response(http.StatusOK, `{"success":true,"data":{"summary":{"disk-a":{"device":{"device_name":"sda","model_name":"ST14000NM001G-2KJ103","capacity":14000519643136,"device_status":0}},"disk-b":{"device":{"device_name":"sdb","model_name":"SSD","capacity":120034123776,"device_status":2}}}}}`), nil
	})})
	if err != nil {
		t.Fatal(err)
	}

	got, err := client.Summary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "sda" || got[0].Model != "ST14000NM001G-2KJ103" || got[0].SizeBytes != 14000519643136 || got[0].State != "healthy" || got[1].State != "failed" {
		t.Fatalf("summary = %#v", got)
	}
}

func TestSummaryPreservesUnknownStatus(t *testing.T) {
	client, err := New(config.ScrutinyConfig{Enabled: true, URL: "http://scrutiny:8080"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"success":true,"data":{"summary":{"disk-a":{"device":{"device_name":"sda","model_name":"disk","capacity":100,"device_status":null}}}}}`), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Summary(context.Background())
	if err != nil || len(got) != 1 || got[0].State != "unknown" {
		t.Fatalf("Summary() = %#v, %v", got, err)
	}
}

func TestSummaryRejectsBadResponses(t *testing.T) {
	for _, tt := range []struct {
		name string
		code int
		body string
	}{{"http", 500, `{}`}, {"json", 200, `{`}, {"failed-envelope", 200, `{"success":false}`}} {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(config.ScrutinyConfig{Enabled: true, URL: "http://scrutiny:8080"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(tt.code, tt.body), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Summary(context.Background()); err == nil {
				t.Fatal("Summary() error = nil")
			}
		})
	}
}

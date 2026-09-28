package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type healthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f healthRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHealthcheckAcceptsOnlySuccessfulHealthResponse(t *testing.T) {
	client := &http.Client{Transport: healthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "http://127.0.0.1:8080/healthz" {
			t.Errorf("URL = %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"status":"ok"}`)), Header: make(http.Header)}, nil
	})}
	if err := healthcheck(context.Background(), client, "http://127.0.0.1:8080/healthz"); err != nil {
		t.Fatalf("healthcheck() error = %v", err)
	}

	client.Transport = healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("not ready")), Header: make(http.Header)}, nil
	})
	if err := healthcheck(context.Background(), client, "http://127.0.0.1:8080/healthz"); err == nil {
		t.Fatal("healthcheck() error = nil for non-200 response")
	}
}

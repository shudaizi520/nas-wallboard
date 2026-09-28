package integration

import (
	"crypto/tls"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestHardenHTTPClientCopiesPolicyAndRequiresTLS12(t *testing.T) {
	originalTransport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS10}}
	original := &http.Client{Transport: originalTransport}
	hardened := HardenHTTPClient(original)
	if hardened == original || hardened.CheckRedirect == nil {
		t.Fatal("client was not copied and hardened")
	}
	transport := hardened.Transport.(*http.Transport)
	if transport == originalTransport || transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("transport = %#v", transport.TLSClientConfig)
	}
	if originalTransport.TLSClientConfig.MinVersion != tls.VersionTLS10 {
		t.Fatal("caller transport was mutated")
	}
	if err := hardened.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v", err)
	}
}

func TestProbeFailureUsesStableTextWithoutLeakingUpstreamDetails(t *testing.T) {
	result := ProbeFailure(errors.New("HTTP 401 rejected token super-secret"))
	if result.Stage != ProbeStageAuthentication || result.OK {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(result.Message, "super-secret") {
		t.Fatalf("secret leaked: %q", result.Message)
	}
}

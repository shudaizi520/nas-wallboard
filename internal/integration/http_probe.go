package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"strings"
)

// HardenHTTPClient copies a caller-owned client, prevents credential-bearing
// redirects, and requires TLS 1.2+ whenever its transport is configurable.
func HardenHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	if copy.Transport == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		copy.Transport = transport
	} else if existing, ok := copy.Transport.(*http.Transport); ok {
		transport := existing.Clone()
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
			if transport.TLSClientConfig.MinVersion < tls.VersionTLS12 {
				transport.TLSClientConfig.MinVersion = tls.VersionTLS12
			}
		}
		copy.Transport = transport
	}
	return &copy
}

// ProbeFailure maps internal/network errors to stable, non-secret public text.
func ProbeFailure(err error) ProbeResult {
	stage := ProbeStageFeature
	message := "服务返回了无效响应"
	var dnsError *net.DNSError
	var certificateError x509.UnknownAuthorityError
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		stage, message = ProbeStageTCP, "连接超时或已取消"
	case errors.As(err, &dnsError):
		stage, message = ProbeStageDNS, "无法解析服务地址"
	case errors.As(err, &certificateError), containsAny(err, "certificate", "tls:"):
		stage, message = ProbeStageTLS, "TLS 证书验证失败"
	case containsAny(err, "unauthorized", "forbidden", "authentication failed", "http 401", "http 403"):
		stage, message = ProbeStageAuthentication, "身份验证失败"
	case containsAny(err, "connect:", "connection refused", "no route to host", "dial tcp"):
		stage, message = ProbeStageTCP, "无法连接服务"
	}
	return ProbeResult{Stage: stage, Message: message}
}

func containsAny(err error, fragments ...string) bool {
	message := strings.ToLower(err.Error())
	for _, fragment := range fragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

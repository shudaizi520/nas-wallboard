package uptimekuma

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
)

const maxResponseBytes = 2 << 20

type Client struct {
	endpoint *url.URL
	apiKey   string
	http     *http.Client
}

func New(cfg config.UptimeKumaConfig, httpClient *http.Client) (*Client, error) {
	endpoint, err := url.Parse(cfg.URL)
	if err != nil || endpoint.Host == "" {
		return nil, errors.New("invalid Uptime Kuma URL")
	}
	return &Client{endpoint: endpoint, apiKey: cfg.APIKey, http: integration.HardenHTTPClient(httpClient)}, nil
}

func (c *Client) Probe(ctx context.Context) integration.ProbeResult {
	if _, err := c.Current(ctx); err != nil {
		return integration.ProbeFailure(err)
	}
	return integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: true, Message: "Uptime Kuma 连接成功"}
}

func (c *Client) Current(ctx context.Context) (model.MonitorStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve("metrics").String(), nil)
	if err != nil {
		return model.MonitorStatus{}, fmt.Errorf("create Uptime Kuma request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	request.SetBasicAuth(c.apiKey, "")
	response, err := c.http.Do(request)
	if err != nil {
		return model.MonitorStatus{}, fmt.Errorf("read Uptime Kuma: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return model.MonitorStatus{}, fmt.Errorf("read Uptime Kuma: HTTP %d", response.StatusCode)
	}
	body, err := readBounded(response.Body)
	if err != nil {
		return model.MonitorStatus{}, fmt.Errorf("read Uptime Kuma response: %w", err)
	}
	return parseMetrics(string(body))
}

func parseMetrics(body string) (model.MonitorStatus, error) {
	result := model.MonitorStatus{DownNames: []string{}}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "monitor_status{") {
			continue
		}
		closing := strings.LastIndexByte(line, '}')
		if closing < len("monitor_status{") {
			return model.MonitorStatus{}, errors.New("invalid Uptime Kuma metrics")
		}
		fields := strings.Fields(strings.TrimSpace(line[closing+1:]))
		if len(fields) == 0 {
			return model.MonitorStatus{}, errors.New("invalid Uptime Kuma monitor status")
		}
		status, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return model.MonitorStatus{}, errors.New("invalid Uptime Kuma monitor status")
		}
		name, ok := prometheusLabel(line[len("monitor_status{"):closing], "monitor_name")
		if !ok || strings.TrimSpace(name) == "" {
			return model.MonitorStatus{}, errors.New("Uptime Kuma monitor name is missing")
		}
		result.Total++
		if status == 0 {
			result.DownNames = append(result.DownNames, strings.TrimSpace(name))
		}
	}
	return result, nil
}

func prometheusLabel(labels, wanted string) (string, bool) {
	for index := 0; index < len(labels); {
		for index < len(labels) && (labels[index] == ' ' || labels[index] == ',') {
			index++
		}
		start := index
		for index < len(labels) && labels[index] != '=' {
			index++
		}
		if index >= len(labels) {
			return "", false
		}
		key := strings.TrimSpace(labels[start:index])
		index++
		if index >= len(labels) || labels[index] != '"' {
			return "", false
		}
		index++
		var value strings.Builder
		for index < len(labels) {
			character := labels[index]
			index++
			if character == '"' {
				if key == wanted {
					return value.String(), true
				}
				break
			}
			if character == '\\' && index < len(labels) {
				escaped := labels[index]
				index++
				switch escaped {
				case 'n':
					value.WriteByte('\n')
				case '\\', '"':
					value.WriteByte(escaped)
				default:
					value.WriteByte(escaped)
				}
				continue
			}
			value.WriteByte(character)
		}
	}
	return "", false
}

func (c *Client) resolve(relative string) *url.URL {
	base := *c.endpoint
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	reference, _ := url.Parse(relative)
	return base.ResolveReference(reference)
}

func readBounded(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("response exceeds size limit")
	}
	return body, nil
}

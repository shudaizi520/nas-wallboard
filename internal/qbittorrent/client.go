package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
)

const maxResponseBytes = 2 << 20

type Client struct {
	endpoint *url.URL
	username string
	password string
	http     *http.Client
}

func New(cfg config.QBittorrentConfig, httpClient *http.Client) (*Client, error) {
	endpoint, err := url.Parse(cfg.URL)
	if err != nil || endpoint.Host == "" {
		return nil, errors.New("invalid qBittorrent URL")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create qBittorrent cookie jar: %w", err)
	}
	secured := integration.HardenHTTPClient(httpClient)
	secured.Jar = jar
	return &Client{endpoint: endpoint, username: cfg.Username, password: cfg.Password, http: secured}, nil
}

func (c *Client) Probe(ctx context.Context) integration.ProbeResult {
	if _, err := c.Current(ctx); err != nil {
		return integration.ProbeFailure(err)
	}
	return integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: true, Message: "qBittorrent 连接成功"}
}

func (c *Client) Current(ctx context.Context) (model.DownloadStatus, error) {
	status, code, err := c.read(ctx)
	if err != nil {
		return model.DownloadStatus{}, err
	}
	if code != http.StatusForbidden && code != http.StatusUnauthorized {
		return status, nil
	}
	if err := c.login(ctx); err != nil {
		return model.DownloadStatus{}, err
	}
	status, code, err = c.read(ctx)
	if err != nil {
		return model.DownloadStatus{}, err
	}
	if code == http.StatusForbidden || code == http.StatusUnauthorized {
		return model.DownloadStatus{}, errors.New("qBittorrent unauthorized")
	}
	return status, nil
}

func (c *Client) read(ctx context.Context) (model.DownloadStatus, int, error) {
	endpoint := c.resolve("api/v2/torrents/info")
	query := endpoint.Query()
	query.Set("filter", "downloading")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return model.DownloadStatus{}, 0, fmt.Errorf("create qBittorrent request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return model.DownloadStatus{}, 0, fmt.Errorf("read qBittorrent: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return model.DownloadStatus{}, response.StatusCode, nil
	}
	if response.StatusCode != http.StatusOK {
		return model.DownloadStatus{}, response.StatusCode, fmt.Errorf("read qBittorrent: HTTP %d", response.StatusCode)
	}
	body, err := boundedBody(response.Body)
	if err != nil {
		return model.DownloadStatus{}, response.StatusCode, fmt.Errorf("read qBittorrent response: %w", err)
	}
	var raw []struct {
		Name     string  `json:"name"`
		Progress float64 `json:"progress"`
		DLSpeed  int64   `json:"dlspeed"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return model.DownloadStatus{}, response.StatusCode, fmt.Errorf("decode qBittorrent response: %w", err)
	}
	result := model.DownloadStatus{Items: []model.DownloadItem{}}
	for _, item := range raw {
		if item.Progress >= 1 {
			continue
		}
		downloadBps := item.DLSpeed
		if downloadBps < 0 {
			downloadBps = 0
		}
		result.ActiveCount++
		result.DownloadBps += downloadBps
		result.Items = append(result.Items, model.DownloadItem{
			Name: strings.TrimSpace(item.Name), ProgressPercent: item.Progress * 100, DownloadBps: downloadBps,
		})
	}
	return result, response.StatusCode, nil
}

func (c *Client) login(ctx context.Context) error {
	form := url.Values{"username": {c.username}, "password": {c.password}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolve("api/v2/auth/login").String(), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create qBittorrent login: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Referer", c.endpoint.String())
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("qBittorrent login: %w", err)
	}
	defer response.Body.Close()
	body, err := boundedBody(response.Body)
	if err != nil {
		return fmt.Errorf("read qBittorrent login: %w", err)
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "Ok." {
		return errors.New("qBittorrent authentication failed")
	}
	return nil
}

func (c *Client) resolve(relative string) *url.URL {
	base := *c.endpoint
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	reference, _ := url.Parse(relative)
	return base.ResolveReference(reference)
}

func boundedBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("response exceeds size limit")
	}
	return bytes.Clone(body), nil
}

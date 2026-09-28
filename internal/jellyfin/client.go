package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"example.com/nas-wallboard/internal/config"
	"example.com/nas-wallboard/internal/integration"
	"example.com/nas-wallboard/internal/model"
)

const maxResponseBytes = 2 << 20

type Client struct {
	endpoint *url.URL
	token    string
	http     *http.Client
}

func New(cfg config.JellyfinConfig, httpClient *http.Client) (*Client, error) {
	endpoint, err := url.Parse(cfg.URL)
	if err != nil || endpoint.Host == "" {
		return nil, errors.New("invalid Jellyfin URL")
	}
	return &Client{endpoint: endpoint, token: cfg.Token, http: integration.HardenHTTPClient(httpClient)}, nil
}

func (c *Client) Probe(ctx context.Context) integration.ProbeResult {
	if _, err := c.Current(ctx); err != nil {
		return integration.ProbeFailure(err)
	}
	return integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: true, Message: "Jellyfin 连接成功"}
}

func (c *Client) Current(ctx context.Context) (model.MediaStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve("Sessions").String(), nil)
	if err != nil {
		return model.MediaStatus{}, fmt.Errorf("create Jellyfin request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Emby-Token", c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return model.MediaStatus{}, fmt.Errorf("read Jellyfin: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return model.MediaStatus{}, fmt.Errorf("read Jellyfin: HTTP %d", response.StatusCode)
	}
	body, err := readBounded(response.Body)
	if err != nil {
		return model.MediaStatus{}, fmt.Errorf("read Jellyfin response: %w", err)
	}
	var raw []struct {
		UserName       string `json:"UserName"`
		DeviceName     string `json:"DeviceName"`
		NowPlayingItem *struct {
			Name       string `json:"Name"`
			SeriesName string `json:"SeriesName"`
		} `json:"NowPlayingItem"`
		PlayState struct {
			IsPaused bool `json:"IsPaused"`
		} `json:"PlayState"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return model.MediaStatus{}, fmt.Errorf("decode Jellyfin response: %w", err)
	}
	result := model.MediaStatus{Sessions: []model.MediaSession{}}
	for _, item := range raw {
		if item.NowPlayingItem == nil {
			continue
		}
		title := mediaTitle(item.NowPlayingItem.SeriesName, item.NowPlayingItem.Name)
		if title == "" {
			continue
		}
		result.Sessions = append(result.Sessions, model.MediaSession{
			Title: title, Device: strings.TrimSpace(item.DeviceName), User: strings.TrimSpace(item.UserName), Paused: item.PlayState.IsPaused,
		})
	}
	return result, nil
}

func (c *Client) resolve(relative string) *url.URL {
	base := *c.endpoint
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	reference, _ := url.Parse(relative)
	return base.ResolveReference(reference)
}

func mediaTitle(series, title string) string {
	series = strings.TrimSpace(series)
	title = strings.TrimSpace(title)
	if series == "" || series == title {
		return title
	}
	if title == "" {
		return series
	}
	return series + " · " + title
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

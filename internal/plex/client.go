package plex

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

func New(cfg config.PlexConfig, httpClient *http.Client) (*Client, error) {
	endpoint, err := url.Parse(cfg.URL)
	if err != nil || endpoint.Host == "" {
		return nil, errors.New("invalid Plex URL")
	}
	return &Client{endpoint: endpoint, token: cfg.Token, http: integration.HardenHTTPClient(httpClient)}, nil
}

func (c *Client) Probe(ctx context.Context) integration.ProbeResult {
	if _, err := c.Current(ctx); err != nil {
		return integration.ProbeFailure(err)
	}
	return integration.ProbeResult{Stage: integration.ProbeStageFeature, OK: true, Message: "Plex 连接成功"}
}

func (c *Client) Current(ctx context.Context) (model.MediaStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve("status/sessions").String(), nil)
	if err != nil {
		return model.MediaStatus{}, fmt.Errorf("create Plex request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Plex-Token", c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return model.MediaStatus{}, fmt.Errorf("read Plex: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return model.MediaStatus{}, fmt.Errorf("read Plex: HTTP %d", response.StatusCode)
	}
	body, err := readBounded(response.Body)
	if err != nil {
		return model.MediaStatus{}, fmt.Errorf("read Plex response: %w", err)
	}
	var raw struct {
		MediaContainer struct {
			Metadata []struct {
				Title            string `json:"title"`
				ParentTitle      string `json:"parentTitle"`
				GrandparentTitle string `json:"grandparentTitle"`
				ViewOffset       int64  `json:"viewOffset"`
				Player           *struct {
					Title    string `json:"title"`
					Product  string `json:"product"`
					Platform string `json:"platform"`
					State    string `json:"state"`
				} `json:"Player"`
				User *struct {
					Title string `json:"title"`
				} `json:"User"`
				Session *struct {
					ID string `json:"id"`
				} `json:"Session"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return model.MediaStatus{}, fmt.Errorf("decode Plex response: %w", err)
	}
	result := model.MediaStatus{Sessions: []model.MediaSession{}}
	for _, item := range raw.MediaContainer.Metadata {
		if item.Player == nil || strings.EqualFold(item.Player.State, "stopped") {
			continue
		}
		title := mediaTitle(item.GrandparentTitle, item.ParentTitle, item.Title)
		if title == "" {
			continue
		}
		user := ""
		if item.User != nil {
			user = strings.TrimSpace(item.User.Title)
		}
		sessionID := ""
		if item.Session != nil {
			sessionID = strings.TrimSpace(item.Session.ID)
		}
		result.Sessions = append(result.Sessions, model.MediaSession{
			Title: title, Device: first(item.Player.Title, item.Player.Product, item.Player.Platform), User: user,
			Paused: strings.EqualFold(item.Player.State, "paused"), SessionID: sessionID, ProgressMillis: item.ViewOffset,
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

func mediaTitle(grandparent, parent, title string) string {
	prefix := first(grandparent, parent)
	title = strings.TrimSpace(title)
	if prefix == "" || prefix == title {
		return title
	}
	if title == "" {
		return prefix
	}
	return prefix + " · " + title
}

func first(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
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

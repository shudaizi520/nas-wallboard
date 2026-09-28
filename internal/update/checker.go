package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const checkFloor = 24 * time.Hour

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type Result struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest,omitempty"`
	URL       string    `json:"url,omitempty"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type Checker struct {
	mu         sync.Mutex
	repository string
	current    string
	client     *http.Client
	clock      func() time.Time
	endpoint   string
	etag       string
	last       Result
	lastCheck  time.Time
}

func New(repository, current string, client *http.Client, clock func() time.Time) (*Checker, error) {
	if !repositoryPattern.MatchString(repository) {
		return nil, errors.New("repository must be owner/name")
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	if clock == nil {
		clock = time.Now
	}
	return &Checker{repository: repository, current: current, client: client, clock: clock, endpoint: "https://api.github.com/repos/" + repository + "/releases/latest"}, nil
}

func (checker *Checker) Check(ctx context.Context) Result {
	checker.mu.Lock()
	defer checker.mu.Unlock()
	now := checker.clock()
	if !checker.lastCheck.IsZero() && now.Sub(checker.lastCheck) < checkFloor {
		return checker.last
	}
	checker.lastCheck = now
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checker.endpoint, nil)
	if err != nil {
		return checker.failure(now)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "nas-wallboard-update-checker")
	if checker.etag != "" {
		request.Header.Set("If-None-Match", checker.etag)
	}
	response, err := checker.client.Do(request)
	if err != nil {
		return checker.failure(now)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified && checker.last.Latest != "" {
		checker.last.CheckedAt = now
		checker.last.Error = ""
		return checker.last
	}
	if response.StatusCode != http.StatusOK {
		return checker.failure(now)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(body) > 64*1024 {
		return checker.failure(now)
	}
	var release struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if json.Unmarshal(body, &release) != nil || strings.TrimSpace(release.Tag) == "" || !strings.HasPrefix(release.URL, "https://github.com/"+checker.repository+"/releases/") {
		return checker.failure(now)
	}
	checker.etag = response.Header.Get("ETag")
	checker.last = Result{Current: checker.current, Latest: release.Tag, URL: release.URL, Available: newer(release.Tag, checker.current), CheckedAt: now}
	return checker.last
}

func (checker *Checker) failure(now time.Time) Result {
	result := checker.last
	result.Current = checker.current
	result.CheckedAt = now
	result.Error = "暂时无法检查更新"
	checker.last = result
	return result
}

func newer(latest, current string) bool {
	var latestMajor, latestMinor, latestPatch, currentMajor, currentMinor, currentPatch int
	if _, err := fmt.Sscanf(strings.TrimPrefix(latest, "v"), "%d.%d.%d", &latestMajor, &latestMinor, &latestPatch); err != nil {
		return latest != current
	}
	if _, err := fmt.Sscanf(strings.TrimPrefix(current, "v"), "%d.%d.%d", &currentMajor, &currentMinor, &currentPatch); err != nil {
		return latest != current
	}
	if latestMajor != currentMajor {
		return latestMajor > currentMajor
	}
	if latestMinor != currentMinor {
		return latestMinor > currentMinor
	}
	return latestPatch > currentPatch
}

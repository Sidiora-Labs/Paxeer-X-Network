package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	githubAPIVersion   = "2022-11-28"
	githubMaxPages     = 100
	githubMaxAttempts  = 3
	maxResponseBytes   = 32 << 20
	errorBodyMaxLength = 512
	userAgent          = "paxeer-flyci-controller"
)

type workflowRun struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type workflowJob struct {
	ID          int64      `json:"id"`
	RunID       int64      `json:"run_id"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	Labels      []string   `json:"labels"`
	CompletedAt *time.Time `json:"completed_at"`
}

type jitConfig struct {
	Runner struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runner"`
	EncodedJITConfig string `json:"encoded_jit_config"`
}

type apiError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s %s: status %d: %s", e.Method, e.Path, e.Status, e.Body)
}

func newAPIError(req *http.Request, status int, body []byte) *apiError {
	text := strings.TrimSpace(string(body))
	if len(text) > errorBodyMaxLength {
		text = text[:errorBodyMaxLength]
	}
	return &apiError{Method: req.Method, Path: req.URL.Path, Status: status, Body: text}
}

type cachedPage struct {
	etag string
	body []byte
	next string
	used bool
}

type githubClient struct {
	baseURL    *url.URL
	owner      string
	repo       string
	token      string
	httpClient *http.Client
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error

	mu           sync.Mutex
	cache        map[string]*cachedPage
	blockedUntil time.Time
}

func newGitHubClient(cfg config, httpClient *http.Client) (*githubClient, error) {
	base, err := url.Parse(cfg.GitHubAPIURL)
	if err != nil {
		return nil, fmt.Errorf("parse GITHUB_API_URL: %w", err)
	}
	return &githubClient{
		baseURL:    base,
		owner:      cfg.Owner,
		repo:       cfg.Repo,
		token:      cfg.GitHubToken,
		httpClient: httpClient,
		now:        time.Now,
		sleep:      sleepContext,
		cache:      make(map[string]*cachedPage),
	}, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *githubClient) repoURL(suffix string, query url.Values) string {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/repos/" + url.PathEscape(c.owner) + "/" + url.PathEscape(c.repo) + suffix
	u.RawPath = ""
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *githubClient) listRuns(ctx context.Context, status string) ([]workflowRun, error) {
	first := c.repoURL("/actions/runs", url.Values{"status": {status}, "per_page": {"100"}})
	var runs []workflowRun
	err := c.getPages(ctx, first, func(body []byte) error {
		var page struct {
			WorkflowRuns []workflowRun `json:"workflow_runs"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("decode workflow runs: %w", err)
		}
		runs = append(runs, page.WorkflowRuns...)
		return nil
	})
	return runs, err
}

func (c *githubClient) listJobs(ctx context.Context, runID int64) ([]workflowJob, error) {
	first := c.repoURL("/actions/runs/"+strconv.FormatInt(runID, 10)+"/jobs", url.Values{"per_page": {"100"}})
	var jobs []workflowJob
	err := c.getPages(ctx, first, func(body []byte) error {
		var page struct {
			Jobs []workflowJob `json:"jobs"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("decode jobs of run %d: %w", runID, err)
		}
		jobs = append(jobs, page.Jobs...)
		return nil
	})
	return jobs, err
}

func (c *githubClient) generateJITConfig(ctx context.Context, name string, labels []string) (jitConfig, error) {
	payload, err := json.Marshal(struct {
		Name          string   `json:"name"`
		RunnerGroupID int64    `json:"runner_group_id"`
		Labels        []string `json:"labels"`
		WorkFolder    string   `json:"work_folder"`
	}{Name: name, RunnerGroupID: 1, Labels: labels, WorkFolder: "_work"})
	if err != nil {
		return jitConfig{}, err
	}
	resp, body, err := c.send(ctx, http.MethodPost, c.repoURL("/actions/runners/generate-jitconfig", nil), payload, "")
	if err != nil {
		return jitConfig{}, err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return jitConfig{}, newAPIError(resp.Request, resp.StatusCode, body)
	}
	var cfg jitConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return jitConfig{}, fmt.Errorf("decode runner configuration for %s: %w", name, err)
	}
	if cfg.EncodedJITConfig == "" {
		return jitConfig{}, fmt.Errorf("runner configuration for %s has no encoded_jit_config", name)
	}
	return cfg, nil
}

func (c *githubClient) getPages(ctx context.Context, first string, visit func([]byte) error) error {
	next := first
	for page := 0; next != ""; page++ {
		if page >= githubMaxPages {
			return fmt.Errorf("more than %d pages following %s", githubMaxPages, first)
		}
		body, following, err := c.getCached(ctx, next)
		if err != nil {
			return err
		}
		if err := visit(body); err != nil {
			return err
		}
		next = following
	}
	return nil
}

func (c *githubClient) getCached(ctx context.Context, pageURL string) ([]byte, string, error) {
	c.mu.Lock()
	cached := c.cache[pageURL]
	etag := ""
	if cached != nil {
		etag = cached.etag
	}
	c.mu.Unlock()

	resp, body, err := c.send(ctx, http.MethodGet, pageURL, nil, etag)
	if err != nil {
		return nil, "", err
	}
	switch {
	case resp.StatusCode == http.StatusNotModified && cached != nil:
		c.mu.Lock()
		cached.used = true
		c.mu.Unlock()
		return cached.body, cached.next, nil
	case resp.StatusCode == http.StatusOK:
		next, err := c.nextLink(resp)
		if err != nil {
			return nil, "", err
		}
		c.mu.Lock()
		if tag := resp.Header.Get("ETag"); tag != "" {
			c.cache[pageURL] = &cachedPage{etag: tag, body: body, next: next, used: true}
		} else {
			delete(c.cache, pageURL)
		}
		c.mu.Unlock()
		return body, next, nil
	default:
		return nil, "", newAPIError(resp.Request, resp.StatusCode, body)
	}
}

func (c *githubClient) nextLink(resp *http.Response) (string, error) {
	for _, header := range resp.Header.Values("Link") {
		for _, part := range strings.Split(header, ",") {
			target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
			if !ok || !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
				continue
			}
			isNext := false
			for _, param := range strings.Split(params, ";") {
				key, val, _ := strings.Cut(strings.TrimSpace(param), "=")
				if strings.EqualFold(key, "rel") && strings.Trim(val, `"`) == "next" {
					isNext = true
				}
			}
			if !isNext {
				continue
			}
			ref, err := url.Parse(strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">"))
			if err != nil {
				return "", fmt.Errorf("parse next link: %w", err)
			}
			resolved := resp.Request.URL.ResolveReference(ref)
			if resolved.Scheme != c.baseURL.Scheme || resolved.Host != c.baseURL.Host {
				return "", fmt.Errorf("next link leaves the API host: %s", resolved.Host)
			}
			return resolved.String(), nil
		}
	}
	return "", nil
}

func (c *githubClient) send(ctx context.Context, method, target string, payload []byte, etag string) (*http.Response, []byte, error) {
	for attempt := 1; ; attempt++ {
		if err := c.waitForRateLimit(ctx); err != nil {
			return nil, nil, err
		}
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
		req.Header.Set("User-Agent", userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %s: %w", method, req.URL.Path, err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("%s %s: read body: %w", method, req.URL.Path, err)
		}
		if c.observeRateLimit(resp) && attempt < githubMaxAttempts {
			continue
		}
		return resp, body, nil
	}
}

func (c *githubClient) waitForRateLimit(ctx context.Context) error {
	c.mu.Lock()
	wait := c.blockedUntil.Sub(c.now())
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	return c.sleep(ctx, wait)
}

func (c *githubClient) observeRateLimit(resp *http.Response) bool {
	remaining := strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining"))
	var resetAt time.Time
	if raw := strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset")); raw != "" {
		if secs, err := strconv.ParseInt(raw, 10, 64); err == nil {
			resetAt = time.Unix(secs, 0)
		}
	}
	var retryAt time.Time
	if raw := strings.TrimSpace(resp.Header.Get("Retry-After")); raw != "" {
		if secs, err := strconv.ParseInt(raw, 10, 64); err == nil && secs >= 0 {
			retryAt = c.now().Add(time.Duration(secs) * time.Second)
		}
	}

	limited := false
	var until time.Time
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		if !retryAt.IsZero() {
			until, limited = retryAt, true
		} else if !resetAt.IsZero() && (remaining == "0" || resp.StatusCode == http.StatusTooManyRequests) {
			until, limited = resetAt, true
		}
	case remaining == "0" && !resetAt.IsZero():
		until = resetAt
	}
	if !until.IsZero() {
		c.mu.Lock()
		if until.After(c.blockedUntil) {
			c.blockedUntil = until
		}
		c.mu.Unlock()
	}
	return limited
}

func (c *githubClient) endPoll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, page := range c.cache {
		if !page.used {
			delete(c.cache, key)
			continue
		}
		page.used = false
	}
}

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	RunnerID    int64      `json:"runner_id"`
	RunnerName  string     `json:"runner_name"`
	ID          int64      `json:"id"`
	RunID       int64      `json:"run_id"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	Labels      []string   `json:"labels"`
	StartedAt   *time.Time `json:"started_at"`
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
	qualificationContract string
	baseURL               *url.URL
	owner                 string
	repo                  string
	token                 string
	httpClient            *http.Client
	now                   func() time.Time
	sleep                 func(context.Context, time.Duration) error

	mu           sync.Mutex
	cache        map[string]*cachedPage
	blockedUntil time.Time
}

func newGitHubClient(cfg config, httpClient *http.Client) (*githubClient, error) {
	base, err := url.Parse(cfg.GitHubAPIURL)
	if err != nil {
		return nil, fmt.Errorf("parse GITHUB_API_URL: %w", err)
	}
	guarded := *httpClient
	guarded.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || (base.Scheme == "https" && req.URL.Scheme != "https") || req.URL.User != nil {
			return errors.New("unsafe provider redirect")
		}
		if req.URL.Host != base.Host {
			req.Header.Del("Authorization")
		}
		return nil
	}
	return &githubClient{
		qualificationContract: cfg.QualificationContract,
		baseURL:               base,
		owner:                 cfg.Owner,
		repo:                  cfg.Repo,
		token:                 cfg.GitHubToken,
		httpClient:            &guarded,
		now:                   time.Now,
		sleep:                 sleepContext,
		cache:                 make(map[string]*cachedPage),
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

func decodeWorkflowJobs(body []byte, runID int64) ([]workflowJob, error) {
	var page struct {
		Jobs []workflowJob `json:"jobs"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("decode jobs of run %d: %w", runID, err)
	}
	if page.Jobs == nil {
		return nil, errors.New("workflow jobs response lacks job inventory")
	}
	seen := make(map[int64]bool)
	for _, job := range page.Jobs {
		if job.ID <= 0 || job.RunID != runID || seen[job.ID] {
			return nil, errors.New("workflow job identity conflicts with run inventory")
		}
		seen[job.ID] = true
	}
	return page.Jobs, nil
}

func (c *githubClient) listJobs(ctx context.Context, runID int64) ([]workflowJob, error) {
	first := c.repoURL("/actions/runs/"+strconv.FormatInt(runID, 10)+"/jobs", url.Values{"per_page": {"100"}, "filter": {"all"}})
	var jobs []workflowJob
	err := c.getPages(ctx, first, func(body []byte) error {
		page, err := decodeWorkflowJobs(body, runID)
		if err != nil {
			return err
		}
		jobs = append(jobs, page...)
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
	if cfg.Runner.ID <= 0 || cfg.Runner.Name != name {
		return jitConfig{}, fmt.Errorf("runner configuration for %s has mismatched runner identity", name)
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

type qualificationRun struct {
	ID           int64  `json:"id"`
	RunAttempt   int64  `json:"run_attempt"`
	HeadSHA      string `json:"head_sha"`
	DisplayTitle string `json:"display_title"`
	Event        string `json:"event"`
	Path         string `json:"path"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
}

func (c *githubClient) qGet(ctx context.Context, suffix string, query url.Values, out any) error {
	resp, b, e := c.send(ctx, http.MethodGet, c.repoURL(suffix, query), nil, "")
	if e != nil {
		return e
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("qualification provider read refused")
	}
	return json.Unmarshal(b, out)
}
func (c *githubClient) qSource(ctx context.Context, r qualificationRequest) error {
	if !qDigest.MatchString(c.qualificationContract) || r.Manifest.ContractSHA256 != c.qualificationContract {
		return errors.New("unapproved qualification contract")
	}
	var commit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		} `json:"commit"`
	}
	if e := c.qGet(ctx, "/commits/"+r.Ref, nil, &commit); e != nil {
		return e
	}
	if commit.SHA != r.Manifest.Revision || commit.Commit.Tree.SHA != r.Manifest.Tree {
		return errors.New("immutable ref moved or candidate tree differs")
	}
	var content struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if e := c.qGet(ctx, "/contents/.github/workflows/paxeer-x-qualification.yml", url.Values{"ref": {r.Manifest.Revision}}, &content); e != nil {
		return e
	}
	b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if e != nil || content.Encoding != "base64" || qHash(b) != r.Manifest.WorkflowSHA256 {
		return errors.New("workflow source mismatch")
	}
	return nil
}
func (c *githubClient) qDispatch(ctx context.Context, r qualificationRequest) error {
	payload := qJSON(map[string]any{"ref": r.Ref, "inputs": map[string]string{"request_id": r.LogicalID, "request": base64.StdEncoding.EncodeToString(qCanonical(r))}})
	if len(payload) > 60000 {
		return errors.New("dispatch input too large")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.repoURL("/actions/workflows/paxeer-x-qualification.yml/dispatches", nil), bytes.NewReader(payload))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	resp, e := c.httpClient.Do(req)
	if e != nil {
		return errors.New("dispatch acknowledgement unknown")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		return errors.New("dispatch acknowledgement not accepted")
	}
	return nil
}
func (c *githubClient) qFind(ctx context.Context, rec qualificationRecord) ([]qualificationRun, error) {
	var runs []qualificationRun
	first := c.repoURL("/actions/workflows/paxeer-x-qualification.yml/runs", url.Values{"event": {"workflow_dispatch"}, "head_sha": {rec.Request.Manifest.Revision}, "per_page": {"100"}})
	e := c.getPages(ctx, first, func(b []byte) error {
		var page struct {
			Runs []qualificationRun `json:"workflow_runs"`
		}
		if e := json.Unmarshal(b, &page); e != nil {
			return e
		}
		for _, run := range page.Runs {
			if run.DisplayTitle == "paxeer-x-qualification/"+rec.Request.LogicalID && run.HeadSHA == rec.Request.Manifest.Revision && run.Event == "workflow_dispatch" && strings.Split(run.Path, "@")[0] == ".github/workflows/paxeer-x-qualification.yml" {
				runs = append(runs, run)
			}
		}
		return nil
	})
	return runs, e
}
func qLogEnvelope(log []byte, binding string) ([]byte, error) {
	chunks := map[int]string{}
	expectedTotal := 0
	expectedDigest := ""
	for _, line := range strings.Split(string(log), "\n") {
		at := strings.Index(line, "PAXEER_X_ENCRYPTED_V1 ")
		if at < 0 {
			continue
		}
		fields := strings.Fields(line[at:])
		if len(fields) != 6 || fields[1] != binding {
			return nil, errors.New("unexpected encrypted log envelope")
		}
		index, e1 := strconv.Atoi(fields[2])
		total, e2 := strconv.Atoi(fields[3])
		if e1 != nil || e2 != nil || total < 1 || total > 256 || index < 0 || index >= total || !qDigest.MatchString(fields[4]) {
			return nil, errors.New("invalid encrypted chunk")
		}
		if expectedTotal != 0 && (total != expectedTotal || fields[4] != expectedDigest) {
			return nil, errors.New("conflicting encrypted envelopes")
		}
		if _, ok := chunks[index]; ok {
			return nil, errors.New("duplicate encrypted chunk")
		}
		expectedTotal = total
		expectedDigest = fields[4]
		chunks[index] = fields[5]
	}
	if expectedTotal == 0 || len(chunks) != expectedTotal {
		return nil, errors.New("missing or truncated encrypted envelope")
	}
	var encoded strings.Builder
	for i := 0; i < expectedTotal; i++ {
		encoded.WriteString(chunks[i])
	}
	b, e := base64.StdEncoding.DecodeString(encoded.String())
	if e != nil || len(b) > qualificationLimit || qHash(b) != expectedDigest {
		return nil, errors.New("encrypted envelope digest mismatch")
	}
	return b, nil
}
func (c *githubClient) qCollect(ctx context.Context, rec qualificationRecord, key *rsa.PrivateKey, signer ed25519.PrivateKey) (qualificationCompleted, error) {
	out := qualificationCompleted{Schema: "paxeer-x.completed-qualification.v1", Request: rec.Request, RunID: rec.RunID, Attempt: rec.Attempt, Repository: c.owner + "/" + c.repo, Workflow: ".github/workflows/paxeer-x-qualification.yml", Status: "completed", Conclusion: "success", ObservedAt: time.Now().UTC(), Domain: "github-actions", ArchiveDigests: map[string]string{}}
	var current qualificationRun
	if e := c.qGet(ctx, "/actions/runs/"+strconv.FormatInt(rec.RunID, 10), nil, &current); e != nil {
		return out, e
	}
	if current.ID != rec.RunID || current.RunAttempt != rec.Attempt || current.HeadSHA != rec.Request.Manifest.Revision || current.Status != "completed" || current.Conclusion != "success" || current.DisplayTitle != "paxeer-x-qualification/"+rec.Request.LogicalID {
		return out, errors.New("run completion changed")
	}
	var jobs []qualificationJobProof
	e := c.getPages(ctx, c.repoURL("/actions/runs/"+strconv.FormatInt(rec.RunID, 10)+"/attempts/"+strconv.FormatInt(rec.Attempt, 10)+"/jobs", url.Values{"per_page": {"100"}}), func(b []byte) error {
		var page struct {
			Jobs []qualificationJobProof `json:"jobs"`
		}
		if e := json.Unmarshal(b, &page); e != nil {
			return e
		}
		jobs = append(jobs, page.Jobs...)
		return nil
	})
	if e != nil {
		return out, e
	}
	for _, cell := range rec.Request.Manifest.Cells {
		var job qualificationJobProof
		matches := 0
		for _, j := range jobs {
			if j.Name == "qualification-"+cell.ID {
				job = j
				matches++
			}
		}
		if matches != 1 {
			return out, errors.New("missing or duplicate matrix job")
		}
		expected := rec.Request.LogicalID + "-" + cell.ID + "-" + strconv.FormatInt(rec.Attempt, 10)
		resp, b, e := c.send(ctx, http.MethodGet, c.repoURL("/actions/jobs/"+strconv.FormatInt(job.ID, 10)+"/logs", nil), nil, "")
		if e != nil || resp.StatusCode != 200 {
			return out, errors.New("completed job log download refused")
		}
		sealed, e := qLogEnvelope(b, expected)
		if e != nil {
			return out, e
		}
		var env qualificationEnvelope
		if e = qStrict(sealed, &env); e != nil {
			return out, e
		}
		plain, e := qOpen(env, expected, key)
		if e != nil {
			return out, e
		}
		var result qualificationResult
		if e = qStrict(plain, &result); e != nil {
			return out, e
		}
		if e = qValidateResult(rec.Request, result, rec.RunID, rec.Attempt, job); e != nil {
			return out, e
		}
		out.Results = append(out.Results, result)
		out.Jobs = append(out.Jobs, job)
		out.ArchiveDigests[cell.ID] = qHash(sealed)
	}
	qSign(&out, signer)
	return out, qValidateCompleted(out, signer.Public().(ed25519.PublicKey), "github-actions", time.Now().Add(time.Minute))
}
func (c *githubClient) qTick(ctx context.Context, registry *qualificationRegistry) error {
	records, e := registry.list()
	if e != nil {
		return e
	}
	key, signer, e := qLoadKeys(registry.root)
	if e != nil {
		return e
	}
	for _, rec := range records {
		switch rec.State {
		case "prepared":
			if e = c.qSource(ctx, rec.Request); e != nil {
				return e
			}
			rec, e = registry.advance(rec.Request.LogicalID, "prepared", "dispatch_intent", 0, 0, "")
			if e != nil {
				return e
			}
			_ = c.qDispatch(ctx, rec.Request)
			_, e = registry.advance(rec.Request.LogicalID, "dispatch_intent", "acknowledgement_unknown", 0, 0, "reconcile before any retry")
			if e != nil {
				return e
			}
		case "dispatch_intent":
			_, e = registry.advance(rec.Request.LogicalID, rec.State, "acknowledgement_unknown", 0, 0, "recovered unresolved intent")
			if e != nil {
				return e
			}
		case "acknowledgement_unknown", "run_bound", "running":
			runs, err := c.qFind(ctx, rec)
			if err != nil {
				return err
			}
			if len(runs) > 1 {
				_, e = registry.advance(rec.Request.LogicalID, rec.State, "conflict", rec.RunID, rec.Attempt, "multiple request runs")
				if e != nil {
					return e
				}
				continue
			}
			if len(runs) == 0 {
				continue
			}
			run := runs[0]
			if rec.RunID != 0 && (rec.RunID != run.ID || rec.Attempt != run.RunAttempt) {
				_, e = registry.advance(rec.Request.LogicalID, rec.State, "conflict", rec.RunID, rec.Attempt, "run attempt changed")
				if e != nil {
					return e
				}
				continue
			}
			if rec.State == "acknowledgement_unknown" {
				_, e = registry.advance(rec.Request.LogicalID, rec.State, "run_bound", run.ID, run.RunAttempt, "")
				if e != nil {
					return e
				}
				continue
			}
			state := "running"
			if run.Status == "completed" {
				state = "terminal_failed"
				if run.Conclusion == "success" {
					state = "completed_uncollected"
				}
			}
			if state != rec.State {
				_, e = registry.advance(rec.Request.LogicalID, rec.State, state, run.ID, run.RunAttempt, "")
				if e != nil {
					return e
				}
			}
		case "completed_uncollected":
			path := filepath.Join(registry.root, rec.Request.LogicalID+".completed.json")
			b, readErr := qRead(path)
			if readErr == nil {
				var existing qualificationCompleted
				if e = qStrict(b, &existing); e != nil {
					return e
				}
				if qHash(qCanonical(existing.Request)) != qHash(qCanonical(rec.Request)) || existing.RunID != rec.RunID || existing.Attempt != rec.Attempt {
					return errors.New("retained completion identity mismatch")
				}
				if e = qValidateCompleted(existing, signer.Public().(ed25519.PublicKey), "github-actions", time.Now().Add(time.Minute)); e != nil {
					return e
				}
			} else {
				if !os.IsNotExist(readErr) {
					return readErr
				}
				completed, err := c.qCollect(ctx, rec, key, signer)
				if err != nil {
					return err
				}
				b = qCanonical(completed)
				if e = qWrite(path, b); e != nil {
					return e
				}
			}
			_, e = registry.advance(rec.Request.LogicalID, rec.State, "completed_validated", rec.RunID, rec.Attempt, qHash(b))
			if e != nil {
				return e
			}
		}
	}
	return nil
}

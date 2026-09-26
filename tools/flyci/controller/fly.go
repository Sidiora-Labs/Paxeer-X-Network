package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type machine struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	State     string        `json:"state"`
	Region    string        `json:"region"`
	CreatedAt time.Time     `json:"created_at"`
	Config    machineConfig `json:"config"`
}

type machineConfig struct {
	Image       string            `json:"image"`
	Env         map[string]string `json:"env,omitempty"`
	Guest       machineGuest      `json:"guest"`
	AutoDestroy bool              `json:"auto_destroy"`
	Restart     machineRestart    `json:"restart"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type machineGuest struct {
	CPUKind  string `json:"cpu_kind"`
	CPUs     int    `json:"cpus"`
	MemoryMB int    `json:"memory_mb"`
}

type machineRestart struct {
	Policy string `json:"policy"`
}

type createMachineRequest struct {
	Name   string        `json:"name"`
	Region string        `json:"region"`
	Config machineConfig `json:"config"`
}

type flyClient struct {
	baseURL    *url.URL
	app        string
	token      string
	httpClient *http.Client
}

func newFlyClient(cfg config, httpClient *http.Client) (*flyClient, error) {
	base, err := url.Parse(cfg.FlyAPIURL)
	if err != nil {
		return nil, fmt.Errorf("parse FLY_API_URL: %w", err)
	}
	return &flyClient{baseURL: base, app: cfg.RunnerApp, token: cfg.FlyToken, httpClient: httpClient}, nil
}

func (c *flyClient) machinesURL(suffix string, query url.Values) string {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/apps/" + url.PathEscape(c.app) + "/machines" + suffix
	u.RawPath = ""
	u.RawQuery = query.Encode()
	return u.String()
}

func (c *flyClient) listMachines(ctx context.Context) ([]machine, error) {
	resp, body, err := c.do(ctx, http.MethodGet, c.machinesURL("", nil), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, newAPIError(resp.Request, resp.StatusCode, body)
	}
	var machines []machine
	if err := json.Unmarshal(body, &machines); err != nil {
		return nil, fmt.Errorf("decode machines of %s: %w", c.app, err)
	}
	return machines, nil
}

func (c *flyClient) createMachine(ctx context.Context, request createMachineRequest) (machine, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return machine{}, err
	}
	resp, body, err := c.do(ctx, http.MethodPost, c.machinesURL("", nil), payload)
	if err != nil {
		return machine{}, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return machine{}, newAPIError(resp.Request, resp.StatusCode, body)
	}
	var created machine
	if err := json.Unmarshal(body, &created); err != nil {
		return machine{}, fmt.Errorf("decode created machine %s: %w", request.Name, err)
	}
	return created, nil
}

func (c *flyClient) destroyMachine(ctx context.Context, id string) error {
	resp, body, err := c.do(ctx, http.MethodDelete, c.machinesURL("/"+url.PathEscape(id), url.Values{"force": {"true"}}), nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return newAPIError(resp.Request, resp.StatusCode, body)
	}
	return nil
}

func (c *flyClient) do(ctx context.Context, method, target string, payload []byte) (*http.Response, []byte, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
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
	return resp, body, nil
}

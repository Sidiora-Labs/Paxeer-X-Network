package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/urfave/cli/v3"
)

var defaultAPI *url.URL

func init() {
	var err error
	defaultAPI, err = url.Parse("http://localhost:1317")
	if err != nil {
		panic(err)
	}
}

var awaitCmd = cli.Command{
	Name:  "await",
	Usage: "Waits for a condition to become true, typically by awaiting success response from a remote API",
	Flags: []cli.Flag{
		&cli.DurationFlag{
			Name:        "timeout",
			Usage:       "The maximum duration to wait for the condition.",
			Value:       time.Minute,
			Destination: &destinations.await.timeout,
		},
	},
	Commands: []*cli.Command{
		{
			Name:                   "validator",
			Usage:                  "Awaits a validator address to be present on chain.",
			MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{outputterFlags},
			Arguments: []cli.Argument{
				&cli.StringArg{
					Name:        "address",
					Destination: &destinations.await.validator.address,
					Config: cli.StringConfig{
						TrimSpace: true,
					},
				},
			},
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:        "api",
					DefaultText: "Automatically inferred from config if present, or falls back to http://localhost:1317",
					Usage:       "The Sei HTTP API URL",
				},
			},
			Action: func(ctx context.Context, command *cli.Command) error {
				if destinations.await.validator.address == "" {
					return fmt.Errorf("validator address must be specified")
				}
				apiUrl, err := getApiUrl(ctx, command)
				if err != nil {
					return fmt.Errorf("getting API URL: %w", err)
				}

				const (
					// TODO: maybe parameterise these as flags.
					attemptTimeout = 5 * time.Second
					backOff        = time.Second
				)

				client := &http.Client{Timeout: attemptTimeout}
				defer client.CloseIdleConnections()
				validatorUrl := apiUrl.JoinPath("cosmos/staking/v1beta1/validators", destinations.await.validator.address)
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, validatorUrl.String(), nil)
				if err != nil {
					return fmt.Errorf("creating validator request: %w", err)
				}

				for start := time.Now(); ctx.Err() == nil; {
					if resp, err := client.Do(request); err == nil {
						all, err := io.ReadAll(resp.Body)
						_ = resp.Body.Close()
						if err != nil {
							return fmt.Errorf("reading response body: %w", err)
						}
						switch resp.StatusCode {
						case http.StatusOK:
							return nil
						case http.StatusBadRequest:
							return fmt.Errorf("bad request: %s", all)
						}
					}
					if time.Since(start) >= destinations.await.timeout {
						return fmt.Errorf("validator not available after %s timeout", destinations.await.timeout)
					}
					time.Sleep(backOff)
				}
				return nil
			},
		},
	},
}

func getApiUrl(ctx context.Context, command *cli.Command) (*url.URL, error) {
	if command.IsSet("api") {
		api, err := url.Parse(command.String("api"))
		if err != nil {
			return nil, fmt.Errorf("parsing api URL: %w", err)
		}
		return api, nil
	}

	if destinations.home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		destinations.home = filepath.Clean(filepath.Join(userHome, ".sei"))
	}

	appTomlPath := filepath.Clean(filepath.Join(destinations.home, "config", "app.toml"))
	appToml, err := os.Open(appTomlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultAPI, nil
		}
		return nil, fmt.Errorf("checking app.toml: %w", err)
	}

	var config struct {
		Api struct {
			Address string `toml:"address"`
		} `toml:"api"`
	}

	if err := toml.NewDecoder(appToml).Decode(&config); err != nil {
		return nil, fmt.Errorf("parsing app.toml: %w", err)
	}
	if config.Api.Address == "" {
		return defaultAPI, nil
	}

	// Massage the returned value, because it configures HTTP server listen address
	// and may not be directly usable.
	apiUrl, err := url.Parse(config.Api.Address)
	if err != nil {
		return nil, fmt.Errorf("parsing configured API address: %w", err)
	}

	if ip := net.ParseIP(apiUrl.Hostname()); ip.IsUnspecified() || ip.IsLoopback() {
		apiUrl.Host = fmt.Sprintf("localhost:%s", apiUrl.Port())
	}
	if strings.ToLower(apiUrl.Scheme) != "https" {
		// Treat any scheme other than HTTPS as http. Because, the config sets the listen
		// address.
		apiUrl.Scheme = "http"
	}
	return apiUrl, nil
}

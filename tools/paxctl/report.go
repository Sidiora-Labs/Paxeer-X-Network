package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seictl/sidecar/shadow"
)

var reportCmd = cli.Command{
	Name:  "report",
	Usage: "Analyze shadow chain comparison data",
	Commands: []*cli.Command{
		&reportDivergenceCmd,
		&reportListCmd,
	},
}

var reportDivergenceCmd = cli.Command{
	Name:  "divergence",
	Usage: "Fetch and render a divergence report from S3",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "env",
			Usage: "Environment shorthand (expands to '{env}-sei-shadow-results')",
		},
		&cli.StringFlag{
			Name:    "bucket",
			Sources: cli.EnvVars("SEI_RESULT_EXPORT_BUCKET"),
			Usage:   "S3 bucket containing the report",
		},
		&cli.StringFlag{
			Name:  "key",
			Usage: "S3 object key (e.g. shadow-results/divergence-198740042.report.json.gz)",
		},
		&cli.IntFlag{
			Name:  "height",
			Usage: "Block height of the divergence report (alternative to --key)",
		},
		&cli.StringFlag{
			Name:    "prefix",
			Sources: cli.EnvVars("SEI_RESULT_EXPORT_PREFIX"),
			Usage:   "S3 key prefix (used with --height to compute key)",
			Value:   "shadow-results/",
		},
		&cli.StringFlag{
			Name:    "region",
			Sources: cli.EnvVars("SEI_RESULT_EXPORT_REGION"),
			Usage:   "AWS region",
			Value:   "eu-central-1",
		},
		&cli.BoolFlag{
			Name:  "json",
			Usage: "Output raw JSON instead of markdown",
		},
	},
	Action: runReportDivergence,
}

func runReportDivergence(ctx context.Context, cmd *cli.Command) error {
	if cmd.IsSet("key") && cmd.IsSet("height") {
		return fmt.Errorf("--key and --height are mutually exclusive")
	}

	bucket := cmd.String("bucket")
	key := cmd.String("key")
	region := cmd.String("region")
	prefix := cmd.String("prefix")

	// When --env or --height is used, resolve to bucket/key.
	if cmd.IsSet("env") || cmd.IsSet("height") {
		resolved, resolvedPrefix, resolvedRegion, err := resolveS3Ref(
			cmd.String("env"), bucket, prefix, region,
		)
		if err != nil {
			return err
		}
		bucket = resolved
		region = resolvedRegion
		if cmd.IsSet("height") {
			key = fmt.Sprintf("%sdivergence-%d.report.json.gz", resolvedPrefix, cmd.Int("height"))
		}
	}

	if bucket == "" {
		return fmt.Errorf("one of --env or --bucket is required")
	}
	if key == "" {
		return fmt.Errorf("one of --key or --height is required")
	}

	downloader, err := seis3.DefaultDownloaderFactory(ctx, region)
	if err != nil {
		return err
	}

	report, err := shadow.FetchReport(ctx, downloader, bucket, key)
	if err != nil {
		return err
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	fmt.Print(shadow.RenderMarkdown(report))
	return nil
}

// resolveS3Ref converts --env/--bucket/--prefix/--region flags to concrete values.
func resolveS3Ref(env, bucket, prefix, region string) (string, string, string, error) {
	switch {
	case env != "" && bucket != "":
		return "", "", "", fmt.Errorf("--env and --bucket are mutually exclusive")
	case env != "":
		bucket = env + "-sei-shadow-results"
	case bucket != "":
		// use as-is
	default:
		return "", "", "", fmt.Errorf("one of --env or --bucket is required")
	}
	if prefix == "" {
		prefix = "shadow-results/"
	}
	if prefix[len(prefix)-1] != '/' {
		prefix += "/"
	}
	if region == "" {
		region = "eu-central-1"
	}
	return bucket, prefix, region, nil
}

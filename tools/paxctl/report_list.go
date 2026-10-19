package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"text/tabwriter"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/urfave/cli/v3"

	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
)

var (
	comparePageRe      = regexp.MustCompile(`(\d+)-(\d+)\.compare\.ndjson\.gz$`)
	divergenceReportRe = regexp.MustCompile(`divergence-(\d+)\.report\.json\.gz$`)
)

var reportListCmd = cli.Command{
	Name:  "list",
	Usage: "List available shadow comparison data in S3",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "env",
			Usage: "Environment shorthand (expands to '{env}-sei-shadow-results')",
		},
		&cli.StringFlag{
			Name:    "bucket",
			Sources: cli.EnvVars("SEI_RESULT_EXPORT_BUCKET"),
			Usage:   "S3 bucket name",
		},
		&cli.StringFlag{
			Name:    "prefix",
			Sources: cli.EnvVars("SEI_RESULT_EXPORT_PREFIX"),
			Usage:   "S3 key prefix",
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
			Usage: "Output raw JSON instead of table",
		},
	},
	Action: runReportList,
}

type listOutput struct {
	Pages             []pageEntry       `json:"pages"`
	DivergenceReports []divergenceEntry `json:"divergenceReports"`
	TotalBlocks       int64             `json:"totalBlocks"`
}

type pageEntry struct {
	Key         string `json:"key"`
	StartHeight int64  `json:"startHeight"`
	EndHeight   int64  `json:"endHeight"`
	Blocks      int    `json:"blocks"`
}

type divergenceEntry struct {
	Key    string `json:"key"`
	Height int64  `json:"height"`
}

func runReportList(ctx context.Context, cmd *cli.Command) error {
	bucket, prefix, region, err := resolveS3Ref(
		cmd.String("env"), cmd.String("bucket"), cmd.String("prefix"), cmd.String("region"),
	)
	if err != nil {
		return err
	}

	lister, err := seis3.DefaultObjectListerFactory(ctx, region)
	if err != nil {
		return err
	}

	// List all objects under prefix.
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	var pages []pageEntry
	var reports []divergenceEntry

	for {
		resp, err := lister.ListObjectsV2(ctx, input)
		if err != nil {
			return fmt.Errorf("listing s3://%s/%s: %w", bucket, prefix, err)
		}
		for _, obj := range resp.Contents {
			key := aws.ToString(obj.Key)
			if m := comparePageRe.FindStringSubmatch(key); len(m) >= 3 {
				start, _ := strconv.ParseInt(m[1], 10, 64)
				end, _ := strconv.ParseInt(m[2], 10, 64)
				pages = append(pages, pageEntry{
					Key: key, StartHeight: start, EndHeight: end,
					Blocks: int(end - start + 1),
				})
			} else if m := divergenceReportRe.FindStringSubmatch(key); len(m) >= 2 {
				height, _ := strconv.ParseInt(m[1], 10, 64)
				reports = append(reports, divergenceEntry{Key: key, Height: height})
			}
		}
		if !aws.ToBool(resp.IsTruncated) {
			break
		}
		input.ContinuationToken = resp.NextContinuationToken
	}

	sort.Slice(pages, func(i, j int) bool { return pages[i].StartHeight < pages[j].StartHeight })
	sort.Slice(reports, func(i, j int) bool { return reports[i].Height < reports[j].Height })

	var totalBlocks int64
	for _, p := range pages {
		totalBlocks += int64(p.Blocks)
	}

	out := listOutput{
		Pages:             pages,
		DivergenceReports: reports,
		TotalBlocks:       totalBlocks,
	}
	if out.Pages == nil {
		out.Pages = []pageEntry{}
	}
	if out.DivergenceReports == nil {
		out.DivergenceReports = []divergenceEntry{}
	}

	if cmd.Bool("json") {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	// Human-readable output.
	fmt.Fprintf(os.Stderr, "%d comparison page(s), %d divergence report(s), %d blocks covered\n\n",
		len(pages), len(reports), totalBlocks)

	if len(pages) == 0 && len(reports) == 0 {
		fmt.Fprintln(os.Stderr, "no data found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tHEIGHT RANGE\tBLOCKS")
	for _, p := range pages {
		fmt.Fprintf(w, "page\t%d - %d\t%d\n", p.StartHeight, p.EndHeight, p.Blocks)
	}
	for _, r := range reports {
		fmt.Fprintf(w, "divergence\t%d\t-\n", r.Height)
	}
	w.Flush()
	return nil
}

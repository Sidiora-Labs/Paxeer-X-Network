package storage

import (
	"errors"
	"regexp"
)

// Policy is deployment input, not proof that provider IAM has been applied.
// Bucket names must identify independently provisioned private resources.
type Policy struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}
type Statement struct {
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource []string `json:"Resource"`
}

func ScopedPolicy(bucket, role string) (Policy, error) {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,62}$`).MatchString(bucket) {
		return Policy{}, errors.New("invalid bucket")
	}
	var actions, prefixes []string
	switch role {
	case "serve":
		actions = []string{"s3:GetObject"}
		prefixes = []string{"approved/*"}
	case "validate-read":
		actions = []string{"s3:GetObject"}
		prefixes = []string{"quarantine/*"}
	case "validate-write":
		actions = []string{"s3:PutObject"}
		prefixes = []string{"approved/*"}
	case "delete":
		actions = []string{"s3:DeleteObject"}
		prefixes = []string{"approved/*", "quarantine/*", "private/*", "temporary/*"}
	default:
		return Policy{}, errors.New("unknown storage role")
	}
	resources := make([]string, len(prefixes))
	for i, p := range prefixes {
		resources[i] = "arn:aws:s3:::" + bucket + "/" + p
	}
	return Policy{"2012-10-17", []Statement{{"Allow", actions, resources}}}, nil
}

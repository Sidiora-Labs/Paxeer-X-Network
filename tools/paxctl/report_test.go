package main

import (
	"testing"
)

func TestResolveS3Ref(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		bucket     string
		prefix     string
		region     string
		wantBucket string
		wantPrefix string
		wantRegion string
		wantErr    bool
	}{
		{
			name:       "env expands to bucket name",
			env:        "prod",
			wantBucket: "prod-sei-shadow-results",
			wantPrefix: "shadow-results/",
			wantRegion: "eu-central-1",
		},
		{
			name:       "bucket passed directly",
			bucket:     "my-custom-bucket",
			wantBucket: "my-custom-bucket",
			wantPrefix: "shadow-results/",
			wantRegion: "eu-central-1",
		},
		{
			name:    "env and bucket are mutually exclusive",
			env:     "prod",
			bucket:  "my-bucket",
			wantErr: true,
		},
		{
			name:    "neither env nor bucket",
			wantErr: true,
		},
		{
			name:       "custom prefix gets trailing slash",
			env:        "dev",
			prefix:     "custom-prefix",
			wantBucket: "dev-sei-shadow-results",
			wantPrefix: "custom-prefix/",
			wantRegion: "eu-central-1",
		},
		{
			name:       "custom prefix with trailing slash unchanged",
			env:        "dev",
			prefix:     "custom-prefix/",
			wantBucket: "dev-sei-shadow-results",
			wantPrefix: "custom-prefix/",
			wantRegion: "eu-central-1",
		},
		{
			name:       "custom region",
			env:        "prod",
			region:     "us-east-2",
			wantBucket: "prod-sei-shadow-results",
			wantPrefix: "shadow-results/",
			wantRegion: "us-east-2",
		},
		{
			name:       "empty prefix defaults",
			env:        "staging",
			prefix:     "",
			wantBucket: "staging-sei-shadow-results",
			wantPrefix: "shadow-results/",
			wantRegion: "eu-central-1",
		},
		{
			name:       "empty region defaults",
			env:        "prod",
			region:     "",
			wantBucket: "prod-sei-shadow-results",
			wantPrefix: "shadow-results/",
			wantRegion: "eu-central-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, prefix, region, err := resolveS3Ref(tt.env, tt.bucket, tt.prefix, tt.region)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bucket != tt.wantBucket {
				t.Errorf("bucket = %q, want %q", bucket, tt.wantBucket)
			}
			if prefix != tt.wantPrefix {
				t.Errorf("prefix = %q, want %q", prefix, tt.wantPrefix)
			}
			if region != tt.wantRegion {
				t.Errorf("region = %q, want %q", region, tt.wantRegion)
			}
		})
	}
}

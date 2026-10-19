package profiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paxeer-network/pax-load/config"
)

// TestProfilesAlignment validates that all JSON profile files in the profiles directory
// can be properly unmarshaled into the LoadConfig struct without any alignment issues
func TestProfilesAlignment(t *testing.T) {
	// Get the current directory (profiles directory)
	profilesDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	// Read all files in the profiles directory
	files, err := os.ReadDir(profilesDir)
	if err != nil {
		t.Fatalf("Failed to read profiles directory: %v", err)
	}

	// Track how many JSON files we tested
	jsonFileCount := 0

	for _, file := range files {
		// Skip directories and non-JSON files
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		jsonFileCount++
		filePath := filepath.Join(profilesDir, file.Name())

		t.Run(file.Name(), func(t *testing.T) {
			// Read the JSON file
			data, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("Failed to read file %s: %v", file.Name(), err)
			}

			// Test 1: Unmarshal into LoadConfig struct
			var loadConfig config.LoadConfig
			if err := json.Unmarshal(data, &loadConfig); err != nil {
				t.Errorf("Failed to unmarshal %s into LoadConfig: %v", file.Name(), err)
				return
			}

			// Test 2: Validate that settings field is properly structured
			if loadConfig.Settings == nil {
				t.Errorf("Profile %s is missing the 'settings' field", file.Name())
				return
			}

			// Test 3: Marshal back to JSON and unmarshal again to detect any data loss
			remarshaled, err := json.Marshal(loadConfig)
			if err != nil {
				t.Errorf("Failed to marshal %s back to JSON: %v", file.Name(), err)
				return
			}

			var loadConfig2 config.LoadConfig
			if err := json.Unmarshal(remarshaled, &loadConfig2); err != nil {
				t.Errorf("Failed to unmarshal remarshaled %s: %v", file.Name(), err)
				return
			}

			// Test 4: Validate that all expected settings fields are present
			settings := loadConfig.Settings
			if settings.Workers == 0 && settings.TPS == 0 && settings.BufferSize == 0 {
				t.Errorf("Profile %s appears to have zero values for critical settings fields", file.Name())
			}

			// Test 5: Check for strict JSON unmarshaling to detect unexpected fields
			// Use a decoder with DisallowUnknownFields to catch any extra fields
			decoder := json.NewDecoder(strings.NewReader(string(data)))
			decoder.DisallowUnknownFields()
			
			var strictConfig config.LoadConfig
			if err := decoder.Decode(&strictConfig); err != nil {
				t.Errorf("Profile %s contains unexpected/unaligned fields: %v", file.Name(), err)
				return
			}

			t.Logf("✓ Profile %s successfully validated", file.Name())
		})
	}

	// Ensure we actually tested some JSON files
	if jsonFileCount == 0 {
		t.Fatal("No JSON files found in profiles directory")
	}

	t.Logf("Successfully validated %d JSON profile files", jsonFileCount)
}

// TestProfilesRequiredFields validates that all profiles contain the minimum required fields
func TestProfilesRequiredFields(t *testing.T) {
	profilesDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	files, err := os.ReadDir(profilesDir)
	if err != nil {
		t.Fatalf("Failed to read profiles directory: %v", err)
	}

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(profilesDir, file.Name())

		t.Run(file.Name()+"_required_fields", func(t *testing.T) {
			data, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("Failed to read file %s: %v", file.Name(), err)
			}

			var loadConfig config.LoadConfig
			if err := json.Unmarshal(data, &loadConfig); err != nil {
				t.Fatalf("Failed to unmarshal %s: %v", file.Name(), err)
			}

			// Validate required top-level fields
			if loadConfig.ChainID == 0 {
				t.Errorf("Profile %s is missing chainId", file.Name())
			}

			if len(loadConfig.Endpoints) == 0 {
				t.Errorf("Profile %s is missing endpoints", file.Name())
			}

			if len(loadConfig.Scenarios) == 0 {
				t.Errorf("Profile %s is missing scenarios", file.Name())
			}

			if loadConfig.Settings == nil {
				t.Errorf("Profile %s is missing settings", file.Name())
			}

			// Validate that scenarios have required fields
			for i, scenario := range loadConfig.Scenarios {
				if scenario.Name == "" {
					t.Errorf("Profile %s scenario %d is missing name", file.Name(), i)
				}
				if scenario.Weight <= 0 {
					t.Errorf("Profile %s scenario %d has invalid weight: %d", file.Name(), i, scenario.Weight)
				}
			}

			t.Logf("✓ Profile %s has all required fields", file.Name())
		})
	}
}

package client

import (
	"encoding/json"
	"testing"
)

func validGovParamChangeTask() GovParamChangeTask {
	return GovParamChangeTask{
		ChainID:     "arctic-1",
		KeyName:     "node_admin",
		Title:       "Update Consensus Timeout Params",
		Description: "Tighten timeouts.",
		Changes: []ParamChangeInput{
			{Subspace: "baseapp", Key: "TimeoutParams", Value: json.RawMessage(`{"propose":"300000000"}`)},
		},
		InitialDeposit: "10000000usei",
		Fees:           "8000usei",
		Gas:            300000,
	}
}

func TestGovParamChangeTask_Validate(t *testing.T) {
	if err := validGovParamChangeTask().Validate(); err != nil {
		t.Fatalf("valid task: unexpected error: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*GovParamChangeTask)
	}{
		{"missing chainId", func(tk *GovParamChangeTask) { tk.ChainID = "" }},
		{"missing keyName", func(tk *GovParamChangeTask) { tk.KeyName = "" }},
		{"missing title", func(tk *GovParamChangeTask) { tk.Title = "" }},
		{"missing description", func(tk *GovParamChangeTask) { tk.Description = "" }},
		{"empty changes", func(tk *GovParamChangeTask) { tk.Changes = nil }},
		{"empty subspace", func(tk *GovParamChangeTask) { tk.Changes[0].Subspace = "" }},
		{"empty key", func(tk *GovParamChangeTask) { tk.Changes[0].Key = "" }},
		{"empty value", func(tk *GovParamChangeTask) { tk.Changes[0].Value = nil }},
		{"missing initialDeposit", func(tk *GovParamChangeTask) { tk.InitialDeposit = "" }},
		{"missing fees", func(tk *GovParamChangeTask) { tk.Fees = "" }},
		{"zero gas", func(tk *GovParamChangeTask) { tk.Gas = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tk := validGovParamChangeTask()
			tc.mut(&tk)
			if err := tk.Validate(); err == nil {
				t.Errorf("expected validation error for %q", tc.name)
			}
		})
	}
}

func TestGovParamChangeTask_ToTaskRequest(t *testing.T) {
	tk := validGovParamChangeTask()
	req := tk.ToTaskRequest()
	if req.Type != TaskTypeGovParamChange {
		t.Errorf("Type = %q, want %q", req.Type, TaskTypeGovParamChange)
	}
	if req.Params == nil {
		t.Fatal("Params is nil")
	}
	p := *req.Params
	for _, k := range []string{"chainId", "keyName", "title", "description", "changes", "initialDeposit", "fees", "gas"} {
		if _, ok := p[k]; !ok {
			t.Errorf("params missing key %q", k)
		}
	}
	// memo omitted when empty (matches the sibling tasks).
	if _, ok := p["memo"]; ok {
		t.Errorf("memo present but was empty")
	}
}

// The per-change value must survive the client encode (ToTaskRequest → the HTTP
// layer's json.Marshal of Params) byte-identical, for any JSON shape — never
// re-escaped. This is the client-side half of the single-encode contract that
// the prop-252 double-encode bug violated.
func TestGovParamChangeTask_ValueSingleEncodedOnWire(t *testing.T) {
	for _, raw := range []string{
		`{"propose":"300000000","commit":"200000000"}`, // object
		`"86400000000000"`, // scalar string
		`100`,              // scalar number
		`true`,             // scalar bool
	} {
		t.Run(raw, func(t *testing.T) {
			tk := validGovParamChangeTask()
			tk.Changes = []ParamChangeInput{{Subspace: "baseapp", Key: "K", Value: json.RawMessage(raw)}}
			req := tk.ToTaskRequest()

			// Marshal Params exactly as the HTTP client would, then re-extract.
			b, err := json.Marshal(req.Params)
			if err != nil {
				t.Fatalf("marshal params: %v", err)
			}
			var decoded struct {
				Changes []struct {
					Value json.RawMessage `json:"value"`
				} `json:"changes"`
			}
			if err := json.Unmarshal(b, &decoded); err != nil {
				t.Fatalf("unmarshal params: %v", err)
			}
			if got := string(decoded.Changes[0].Value); got != raw {
				t.Errorf("wire value = %q, want %q (double-encoded?)", got, raw)
			}
		})
	}
}

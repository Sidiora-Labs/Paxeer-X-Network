package client

import (
	"strings"
	"testing"
)

const (
	validSeiAddr1 = "sei1zg69v7y6hn00qy352euf40x77qfrg4nclsjzp9"
	validSeiAddr2 = "sei140x77qfrg4ncn27dauqjx3t83x4ummcpmrsjjl"
)

func validNonForkTask(accounts []GenesisAccountEntry) AssembleAndUploadGenesisTask {
	return AssembleAndUploadGenesisTask{
		AccountBalance: "1000usei",
		Namespace:      "default",
		Nodes:          []GenesisNodeParam{{Name: "node-0"}},
		Accounts:       accounts,
	}
}

func TestAssembleAndUploadGenesisTask_ValidateAccounts(t *testing.T) {
	cases := []struct {
		name     string
		accounts []GenesisAccountEntry
		wantErr  string
	}{
		{name: "no accounts", accounts: nil},
		{name: "valid single", accounts: []GenesisAccountEntry{{Address: validSeiAddr1, Balance: "1usei"}}},
		{name: "valid multiple", accounts: []GenesisAccountEntry{{Address: validSeiAddr1, Balance: "1usei"}, {Address: validSeiAddr2, Balance: "2usei"}}},
		{name: "missing address", accounts: []GenesisAccountEntry{{Balance: "1usei"}}, wantErr: "missing required field Address"},
		{name: "missing balance", accounts: []GenesisAccountEntry{{Address: validSeiAddr1}}, wantErr: "missing required field Balance"},
		{name: "wrong hrp", accounts: []GenesisAccountEntry{{Address: "cosmos1zg69v7y6hn00qy352euf40x77qfrg4ncjur58y", Balance: "1usei"}}, wantErr: `hrp "cosmos"`},
		{name: "bad checksum", accounts: []GenesisAccountEntry{{Address: "sei1zg69v7y6hn00qy352euf40x77qfrg4nclsjzpz", Balance: "1usei"}}, wantErr: "address"},
		{name: "not bech32", accounts: []GenesisAccountEntry{{Address: "junk", Balance: "1usei"}}, wantErr: "address"},
		{name: "valid vesting", accounts: []GenesisAccountEntry{{Address: validSeiAddr1, Balance: "2usei", Vesting: &GenesisAccountVesting{Amount: "1usei", EndTime: 1893456000}}}},
		{name: "vesting missing amount", accounts: []GenesisAccountEntry{{Address: validSeiAddr1, Balance: "1usei", Vesting: &GenesisAccountVesting{EndTime: 1893456000}}}, wantErr: "vesting missing required field Amount"},
		{name: "vesting non-positive end time", accounts: []GenesisAccountEntry{{Address: validSeiAddr1, Balance: "1usei", Vesting: &GenesisAccountVesting{Amount: "1usei"}}}, wantErr: "EndTime must be a positive unix timestamp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validNonForkTask(tc.accounts).Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error: got %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestAssembleAndUploadGenesisTask_ToTaskRequest_OmitsEmpty(t *testing.T) {
	req := validNonForkTask(nil).ToTaskRequest()
	if req.Params == nil {
		t.Fatal("Params nil")
	}
	if _, present := (*req.Params)["accounts"]; present {
		t.Errorf("nil accounts should omit field; got: %+v", *req.Params)
	}
}

func TestAssembleAndUploadGenesisTask_ToTaskRequest_SerializesAccounts(t *testing.T) {
	accs := []GenesisAccountEntry{{Address: validSeiAddr1, Balance: "1000usei"}}
	req := validNonForkTask(accs).ToTaskRequest()
	if req.Type != TaskTypeAssembleGenesis {
		t.Errorf("Type: got %q, want %q", req.Type, TaskTypeAssembleGenesis)
	}
	got, ok := (*req.Params)["accounts"].([]interface{})
	if !ok || len(got) != 1 {
		t.Fatalf("accounts: %+v", (*req.Params)["accounts"])
	}
	entry := got[0].(map[string]interface{})
	if entry["address"] != validSeiAddr1 || entry["balance"] != "1000usei" {
		t.Errorf("entry: got %+v", entry)
	}
	if _, present := entry["vesting"]; present {
		t.Errorf("non-vesting account should omit vesting key; got %+v", entry)
	}
}

// genesisAccountsToWire hand-builds the request map, a second serializer of the
// same object independent of the struct's json tags, so this pins the keys and
// value types it emits. The server unmarshals into the same wire type, so the
// tags themselves need no cross-package check.
func TestAssembleAndUploadGenesisTask_ToTaskRequest_SerializesVesting(t *testing.T) {
	accs := []GenesisAccountEntry{{
		Address: validSeiAddr1,
		Balance: "2000000usei",
		Vesting: &GenesisAccountVesting{Amount: "1000000usei", EndTime: 1893456000, Delayed: true},
	}}
	req := validNonForkTask(accs).ToTaskRequest()

	got := (*req.Params)["accounts"].([]interface{})
	entry := got[0].(map[string]interface{})
	vesting, ok := entry["vesting"].(map[string]interface{})
	if !ok {
		t.Fatalf("vesting key: got %+v", entry["vesting"])
	}
	if vesting["amount"] != "1000000usei" || vesting["endTime"] != int64(1893456000) || vesting["delayed"] != true {
		t.Errorf("vesting map: got %+v", vesting)
	}
}

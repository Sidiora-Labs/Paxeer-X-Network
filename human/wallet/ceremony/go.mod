module github.com/sidiora-labs/paxeer-network/human/wallet/ceremony

go 1.25.6

require (
	filippo.io/edwards25519 v1.1.0
	github.com/ethereum/go-ethereum v1.16.8
	github.com/getamis/alice v1.0.9-0.20260916062408-d8fd6861d3b2
	github.com/jackc/pgx/v5 v5.11.0
	github.com/lib/pq v1.10.9
	github.com/sidiora-labs/paxeer-network/human/wallet/attestor v0.0.0
	golang.org/x/crypto v0.54.0
)

require (
	github.com/ProjectZKM/Ziren/crates/go-runtime/zkvm_runtime v0.0.0-20251001021608-1fe7b43fc4d6 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/bits-and-blooms/bitset v1.24.3 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.5 // indirect
	github.com/consensys/gnark-crypto v0.18.0 // indirect
	github.com/crate-crypto/go-eth-kzg v1.4.0 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/decred/dcrd/dcrec/edwards v1.0.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.5 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/shirou/gopsutil v3.21.11+incompatible // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/supranational/blst v0.3.16-0.20250831170142-f48500c1fdbe // indirect
	github.com/tklauser/go-sysconf v0.3.15 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	gonum.org/v1/gonum v0.7.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/sidiora-labs/paxeer-network => ../../..

replace github.com/sidiora-labs/paxeer-network/human/wallet/attestor => ../attestor

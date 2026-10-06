# crypto

crypto is the cryptographic package adapted for Tendermint's uses. In this tree it provides Ed25519 keys ([`ed25519`](./ed25519/)), SHA-256 hashing ([`tmhash`](./tmhash/)) and simple Merkle trees ([`merkle`](./merkle/)).

## Importing it

To get the interfaces,
`import "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto"`

For any specific algorithm, use its specific module e.g.
`import "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/ed25519"`

## Binary encoding

For Binary encoding, please refer to the [encoding specification](../spec/core/encoding.md).

## JSON Encoding

JSON encoding is done using the engine's internal json encoder ([`libs/json`](../libs/json/doc.go)); key types register their type tags with [`internal/jsontypes`](../internal/jsontypes/).

```go
Example JSON encodings:

ed25519.SecretKey   - {"type":"tendermint/PrivKeyEd25519","value":"EVkqJO/jIXp3rkASXfh9YnyToYXRXhBr6g9cQVxPFnQBP/5povV4HTjvsy530kybxKHwEi85iU8YL0qQhSYVoQ=="}
ed25519.PublicKey   - {"type":"tendermint/PubKeyEd25519","value":"AT/+aaL1eB0477Mud9JMm8Sh8BIvOYlPGC9KkIUmFaE="}
```

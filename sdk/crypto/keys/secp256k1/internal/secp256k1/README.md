# secp256k1

This package is copied from the `crypto/secp256k1` package of go-ethereum (`github.com/ethereum/go-ethereum`).

Unlike the rest of go-ethereum it is 3-clause BSD licensed (see [`LICENSE`](LICENSE)), so it is compatible with our Apache 2.0 license. It is copied here rather than depending on go-ethereum to avoid issues with vendoring of the GPL parts of that repository by downstream.

It wraps the C library in [`libsecp256k1/`](libsecp256k1/README.md) through cgo.

package lxwire

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const (
	BindDomain        = "LX:PAXEER-BIND:v1"
	BindMessageLength = len(BindDomain) + 32 + 20 + 8
)

var ErrBindMessage = errors.New("lxwire: not a LX:PAXEER-BIND:v1 message")

type Binding struct {
	ChainID    uint64
	EVMAddress [20]byte
	Nonce      uint64
}

func BindMessage(chainID uint64, evm [20]byte, nonce uint64) []byte {
	message := make([]byte, 0, BindMessageLength)
	message = append(message, BindDomain...)
	var chain [32]byte
	binary.BigEndian.PutUint64(chain[24:], chainID)
	message = append(message, chain[:]...)
	message = append(message, evm[:]...)
	return binary.BigEndian.AppendUint64(message, nonce)
}

func ParseBindMessage(message []byte) (Binding, error) {
	var binding Binding
	if len(message) != BindMessageLength || !bytes.HasPrefix(message, []byte(BindDomain)) {
		return binding, ErrBindMessage
	}
	rest := message[len(BindDomain):]
	chain := rest[:32]
	for _, b := range chain[:24] {
		if b != 0 {
			return binding, ErrBindMessage
		}
	}
	binding.ChainID = binary.BigEndian.Uint64(chain[24:])
	copy(binding.EVMAddress[:], rest[32:52])
	binding.Nonce = binary.BigEndian.Uint64(rest[52:])
	return binding, nil
}

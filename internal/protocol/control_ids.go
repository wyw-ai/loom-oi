package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
)

var ErrControlIDExhausted = errors.New("control session identifiers exhausted")

// ControlIDs produces opaque 128-bit identifiers without an ever-growing set
// of used IDs. AES is a keyed permutation, so distinct counters cannot produce
// the same identifier in a session. Each session has an independent random key.
// The session's coordinator serializes Next; do not copy or share this generator
// between sessions. Exhaustion requires ending the session, never resetting it.
type ControlIDs struct {
	block   cipher.Block
	counter uint64
}

func NewControlIDs() (*ControlIDs, error) { return newControlIDs(rand.Reader) }

func newControlIDs(source io.Reader) (*ControlIDs, error) {
	var key [32]byte
	if _, err := io.ReadFull(source, key[:]); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key[:])
	clear(key[:])
	if err != nil {
		return nil, err
	}
	return &ControlIDs{block: block}, nil
}

func (g *ControlIDs) Next() (string, error) {
	if g == nil || g.block == nil || g.counter == math.MaxUint64 {
		return "", ErrControlIDExhausted
	}
	g.counter++
	var input, output [aes.BlockSize]byte
	binary.BigEndian.PutUint64(input[8:], g.counter)
	g.block.Encrypt(output[:], input[:])
	return hex.EncodeToString(output[:]), nil
}

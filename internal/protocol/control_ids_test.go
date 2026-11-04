package protocol

import (
	"bytes"
	"errors"
	"math"
	"regexp"
	"testing"
)

func TestControlIDsBoundedUnique(t *testing.T) {
	g, err := NewControlIDs()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	format := regexp.MustCompile(`^[0-9a-f]{32}$`)
	for range 10000 {
		id, err := g.Next()
		if err != nil || !format.MatchString(id) || seen[id] {
			t.Fatalf("reused/invalid id %q %v", id, err)
		}
		seen[id] = true
	}
	other, err := NewControlIDs()
	if err != nil {
		t.Fatal(err)
	}
	id, err := other.Next()
	if err != nil || seen[id] {
		t.Fatal("independent session reused random-key sequence", err)
	}
	g.counter = math.MaxUint64 - 1
	if _, err := g.Next(); err != nil {
		t.Fatal("last unique ID rejected", err)
	}
	for range 2 {
		id, err := g.Next()
		if id != "" || !errors.Is(err, ErrControlIDExhausted) || g.counter != math.MaxUint64 {
			t.Fatal("identifier overflow reset or mutated generator")
		}
	}
}

func TestControlIDsInitializationFailure(t *testing.T) {
	if g, err := newControlIDs(bytes.NewReader([]byte("short random input"))); err == nil || g != nil {
		t.Fatal("partial entropy accepted")
	}
	for _, g := range []*ControlIDs{nil, {}} {
		if _, err := g.Next(); !errors.Is(err, ErrControlIDExhausted) {
			t.Fatal("uninitialized generator accepted")
		}
	}
}

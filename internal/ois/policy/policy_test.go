package policy

import (
	"testing"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
)

func TestSelectors(t *testing.T) {
	for _, s := range []string{"lab-a/http", "lab-a/*"} {
		if !ValidSelector(s) {
			t.Errorf("%s rejected", s)
		}
	}
	for _, s := range []string{"", "*", "lab-a", "LAB/*", "lab-a/**", "/*"} {
		if ValidSelector(s) {
			t.Errorf("%s accepted", s)
		}
	}
	if !Match("lab-a/*", "lab-a/http") || Match("lab-a/*", "lab-b/http") || Match("lab-a/*", "bad") ||
		!Match("lab-a/http", "lab-a/http") || Match("lab-a/http", "lab-a/alt") {
		t.Fatal("match")
	}
}

func TestActions(t *testing.T) {
	if !ValidActions([]string{"discover"}) || !ValidActions([]string{"discover", "open"}) {
		t.Fatal("valid rejected")
	}
	for _, a := range [][]string{nil, {}, {"x"}, {"open", "open"}} {
		if ValidActions(a) {
			t.Errorf("%v accepted", a)
		}
	}
}

func TestAllowed(t *testing.T) {
	ps := []store.Policy{
		{Subject: "alice", ExposureSelector: "lab-a/*", Actions: []string{"discover"}, Enabled: true},
		{Subject: "alice", ExposureSelector: "lab-a/http", Actions: []string{"discover", "open"}, Enabled: true},
		{Subject: "bob", ExposureSelector: "lab-a/*", Actions: []string{"open"}, Enabled: false},
	}
	cases := []struct {
		subject, exposure, action string
		want                      bool
	}{
		{"alice", "lab-a/http", "open", true},
		{"alice", "lab-a/alt", "discover", true},
		{"alice", "lab-a/alt", "open", false},
		{"alice", "lab-b/http", "discover", false},
		{"bob", "lab-a/http", "open", false},
	}
	for _, c := range cases {
		if got := Allowed(ps, c.subject, c.exposure, c.action); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

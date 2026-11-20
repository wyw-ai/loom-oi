package contract

import "testing"

func TestStrictParsing(t *testing.T) {
	for _, s := range []string{`{"a":1,"\u0061":2}`, `{"list":[{"key":1,"key":2}]}`, `{"s":"\ud801"}`, `{"s":"\udc00"}`, "{\"s\":\"\xff\"}"} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("accepted malformed JSON %q", s)
		}
	}
	for _, s := range []string{`{"s":"\ud801\udc00"}`, `{"s":"\ufffd"}`, `{"a":{"x":1},"b":{"x":2}}`, `{"a":"\\ud800"}`} {
		if _, err := Parse([]byte(s)); err != nil {
			t.Errorf("rejected valid JSON %q: %v", s, err)
		}
	}
}

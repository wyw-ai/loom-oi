package protocol

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// ValidateJSON runs before the first decoding of an untrusted payload.
// encoding/json accepts duplicate object fields and silently replaces malformed
// Unicode, so neither property can be verified from an already decoded map.
func ValidateJSON(raw []byte) error {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return errors.New("invalid JSON or UTF-8 encoding")
	}
	// json.Valid already checked delimiters, escapes and the single-value
	// boundary. Walk only strings and object boundaries to check the properties
	// that Go's JSON parser intentionally permits.
	var objects []map[string]struct{}
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '{':
			objects = append(objects, make(map[string]struct{}))
		case '}':
			objects = objects[:len(objects)-1]
		case '"':
			start := i
			escaped := false
			for i++; raw[i] != '"'; i++ {
				if raw[i] != '\\' {
					continue
				}
				escaped = true
				i++
				if raw[i] != 'u' {
					continue
				}
				unit := hexUnit(raw[i+1 : i+5])
				i += 4
				if unit < 0xd800 || unit > 0xdfff {
					continue
				}
				if unit > 0xdbff || i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
					return errors.New("invalid Unicode surrogate")
				}
				low := hexUnit(raw[i+3 : i+7])
				if low < 0xdc00 || low > 0xdfff {
					return errors.New("invalid Unicode surrogate pair")
				}
				i += 6
			}
			end := i + 1
			next := end
			for next < len(raw) && (raw[next] == ' ' || raw[next] == '\t' || raw[next] == '\r' || raw[next] == '\n') {
				next++
			}
			if next == len(raw) || raw[next] != ':' {
				continue
			}
			key := string(raw[start+1 : i])
			if escaped {
				_ = json.Unmarshal(raw[start:end], &key)
			}
			keys := objects[len(objects)-1]
			if _, exists := keys[key]; exists {
				return errors.New("duplicate JSON object member")
			}
			keys[key] = struct{}{}
		}
	}
	return nil
}

// The caller has already validated the JSON escape and its four hex digits.
func hexUnit(digits []byte) uint16 {
	var value uint16
	for _, c := range digits {
		value <<= 4
		if c <= '9' {
			value += uint16(c - '0')
		} else {
			value += uint16((c | 0x20) - 'a' + 10)
		}
	}
	return value
}

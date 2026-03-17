package transform

import (
	"bytes"
	"encoding/ascii85"
)

// Characters set to use during encoding.
type Base85Charset int

const (
	// The difference from standard Ascii85 charset is that this charset doesn't
	// contain characters that require backslashing or have special meaning in URL.
	//
	// However, this charset still may be misinterpreted by some programs. So double-check.
	//
	// In UTF-8 encoding this charset occupy at max 1 byte per character.
	Base85CharsetASCII Base85Charset = iota

	// Contain only numbers, English letters and Russian letters.
	//
	// In UTF-8 encoding this charset occupy at max 2 bytes per character.
	Base85CharsetRU
)

var (
	base85CharsetStd   = []rune("!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstu")
	base85CharsetASCII = []rune("!v#$%}x()*+,-.{0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[w]^_yabcdefghijklmnopqrstu")
	base85CharsetRU    = []rune("абвгдеёжзийклмн0123456789опрстуфABCDEFGHIJKLMNOPQRSTUVWXYZхцчшщъabcdefghijklmnopqrstu")
)

// ToBase85 encodes in bytes into string using 85 characters from the set.
// To decode back use FromBase85.
func ToBase85(in []byte, set Base85Charset) string {
	dst := make([]byte, ascii85.MaxEncodedLen(len(in)))
	n := ascii85.Encode(dst, in)
	dst = dst[:n]

	var setOld, setNew []rune
	setOld = base85CharsetStd

	switch set {
	case Base85CharsetASCII:
		setNew = base85CharsetASCII
	case Base85CharsetRU:
		setNew = base85CharsetRU
	default:
		return ""
	}

	dst = bytes.Map(base85Mapping(setOld, setNew), dst)
	out := string(dst)

	return out
}

// FromBase85 decodes in string that was encoded using ToBase85.
// The set is detected automatically.
func FromBase85(in string) ([]byte, error) {
	setOld := base85CharsetASCII
	setNew := base85CharsetStd

	for _, r := range in {
		if r > 127 {
			setOld = base85CharsetRU
			break
		}
	}

	src := []byte(in)
	src = bytes.Map(base85Mapping(setOld, setNew), src)

	dst := make([]byte, ascii85.MaxEncodedLen(len(in)))
	n, _, err := ascii85.Decode(dst, src, true)
	out := dst[:n]

	return out, err
}

func base85Mapping(setOld, setNew []rune) func(r rune) rune {
	if len(setOld) != len(setNew) {
		return func(r rune) rune {
			return '0'
		}
	}

	return func(r rune) rune {
		for i, rOld := range setOld {
			rNew := setNew[i]

			if r == rOld {
				return rNew
			}
		}

		return r
	}
}

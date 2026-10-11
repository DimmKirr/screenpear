package mask

import (
	"hash/fnv"
	"strings"
	"unicode"
)

// Substitute returns generated replacement text with the shape of text:
// same rune count, letters become pronounceable letters with the same case,
// digits become other digits, everything else stays. Deterministic per
// (text, seed). It is the Swift Substitute.text mirror (build/swift-shared);
// the two generators agree on shape, not on the exact letters.
func Substitute(text string, seed int) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	rng := xorshift{state: h.Sum64() ^ uint64(seed)*0x9E3779B97F4A7C15}
	consonants := []rune("bcdfghklmnprstvz")
	vowels := []rune("aeiou")
	var b strings.Builder
	wantVowel := rng.next()%2 == 0
	for _, r := range text {
		switch {
		case unicode.IsLetter(r):
			pool := consonants
			if wantVowel {
				pool = vowels
			}
			c := pool[rng.next()%uint64(len(pool))]
			if unicode.IsUpper(r) {
				c = unicode.ToUpper(c)
			}
			b.WriteRune(c)
			wantVowel = !wantVowel
		case unicode.IsDigit(r):
			b.WriteRune(rune('0' + rng.next()%10))
		default:
			b.WriteRune(r)
			wantVowel = rng.next()%2 == 0
		}
	}
	return b.String()
}

type xorshift struct{ state uint64 }

func (x *xorshift) next() uint64 {
	if x.state == 0 {
		x.state = 0x2545F4914F6CDD1D
	}
	x.state ^= x.state << 13
	x.state ^= x.state >> 7
	x.state ^= x.state << 17
	return x.state
}

func hasWordChars(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

package store

import (
	"strings"
	"unicode"
)

// postgresFTSQuery treats punctuation as separators, never as tsquery syntax, and preserves
// the existing AND/prefix search behavior. simple dictionaries work across the UI's languages.
func postgresFTSQuery(q string) string {
	words := strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	for i, w := range words {
		words[i] = "'" + w + "':*"
	}
	return strings.Join(words, " & ")
}

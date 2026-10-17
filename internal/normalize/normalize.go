// Package normalize builds the comparison keys used to detect duplicate products.
//
// A key is never shown to anyone: it only exists to compare a seller's entry
// with the catalog. The original text is always kept elsewhere.
package normalize

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Text returns the comparison form of s:
//
//   - accents are removed (NFKD + drop combining marks): "Câmera" → "camera";
//   - letters are lower-cased: "BLACK+DECKER" → "black decker";
//   - apostrophes and double quotes are deleted, not replaced by a space,
//     so "Levi's" → "levis" and 12.9 followed by a double quote or by two
//     apostrophes → "12 9";
//   - every other run of non-alphanumeric characters becomes a single space,
//     which also collapses repeated spaces: "Xbox  Series X" → "xbox series x";
//   - leading and trailing spaces are trimmed.
func Text(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// combining mark left by NFKD: the accent itself
		case r == '\'' || r == '"' || r == '’' || r == '‘' || r == '“' || r == '”':
			// deleted so that "Levi's" and "Levis" compare equal
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			b.WriteRune(unicode.ToLower(r))
		default:
			pendingSpace = true
		}
	}
	return b.String()
}

// Brand is Text for a nullable brand. A missing brand becomes "", never NULL:
// in SQL `NULL = NULL` is not true and NULLs never collide in a UNIQUE index,
// so unbranded products would otherwise always look new.
func Brand(brand *string) string {
	if brand == nil {
		return ""
	}
	return Text(*brand)
}

// Package similarity scores how close two normalized product names are.
package similarity

import "strings"

// Jaccard returns |A ∩ B| / |A ∪ B| over the distinct words of a and b,
// which must already be normalized. It is 1 for identical word sets and 0
// when nothing is shared. Two empty names score 0: an empty name says nothing
// about identity.
//
// Words, not characters, are compared on purpose: product names are made of
// words (type, model, variant) and swapping one of them matters more than a
// long shared suffix. On the catalog, "colander stainless steel" and
// "ladle stainless steel" score 0.89 at character level but 0.5 here.
func Jaccard(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 && len(wb) == 0 {
		return 0
	}
	shared := 0
	for w := range wa {
		if wb[w] {
			shared++
		}
	}
	return float64(shared) / float64(len(wa)+len(wb)-shared)
}

func words(s string) map[string]bool {
	set := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		set[w] = true
	}
	return set
}

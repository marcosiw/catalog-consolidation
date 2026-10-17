// Package rules holds catalog content policy applied before matching.
//
// This is not a security control: SQL injection is prevented by parameterized
// statements in the store. The exclusion list keeps known unwanted items, such
// as test products pushed by sellers to production, out of the catalog.
package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ExclusionList rejects entries whose name or brand contains a listed term.
type ExclusionList struct {
	terms    []string // as written in the file, reported back to the user
	compared []string // comparison form of terms
}

// NewExclusionList builds a list from terms. Terms must be specific: matching
// is by containment anywhere, so "test" alone would also match "Contest".
func NewExclusionList(terms []string) (*ExclusionList, error) {
	l := &ExclusionList{}
	for _, t := range terms {
		c := foldCase(t)
		if c == "" {
			return nil, fmt.Errorf("exclusion list: empty term")
		}
		l.terms = append(l.terms, t)
		l.compared = append(l.compared, c)
	}
	return l, nil
}

// LoadExclusionList reads a JSON file of the form {"terms": ["..."]}.
func LoadExclusionList(path string) (*ExclusionList, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read exclusion list: %w", err)
	}
	var file struct {
		Terms []string `json:"terms"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse exclusion list %s: %w", path, err)
	}
	return NewExclusionList(file.Terms)
}

// Match returns the first term contained in any of fields, and whether one was
// found. Comparison ignores case and repeated spaces but keeps punctuation, so
// "SELECT 1;" matches "select  1;" and not "Select 1 Kit".
func (l *ExclusionList) Match(fields ...string) (string, bool) {
	for _, f := range fields {
		cf := foldCase(f)
		for i, term := range l.compared {
			if strings.Contains(cf, term) {
				return l.terms[i], true
			}
		}
	}
	return "", false
}

func foldCase(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

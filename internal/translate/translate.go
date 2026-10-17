// Package translate maps seller product names to the catalog language.
//
// Translation is only used to confirm a candidate already selected by
// similarity, and only by exact equality, so a wrong translation cannot merge
// two products on its own.
package translate

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/marcosiw/catalog-consolidation/internal/normalize"
)

// Translator turns a normalized name into the normalized name it would have
// in the catalog language. It returns the input unchanged when it has nothing
// to translate. An LLM or a translation API can implement it later.
type Translator interface {
	Translate(normalizedName string) string
}

// Glossary is a word-by-word Translator. Brands and model codes are never in
// it, so they pass through untouched.
type Glossary struct {
	words map[string]string
}

// NewGlossary builds a Glossary from source → target pairs. Both sides are
// normalized, so entries may be written with accents and capitals.
func NewGlossary(pairs map[string]string) (*Glossary, error) {
	g := &Glossary{words: make(map[string]string, len(pairs))}
	for from, to := range pairs {
		f, t := normalize.Text(from), normalize.Text(to)
		if f == "" || t == "" || strings.Contains(f, " ") {
			return nil, fmt.Errorf("glossary entry %q → %q: source must be a single word and target non-empty", from, to)
		}
		g.words[f] = t
	}
	return g, nil
}

// LoadGlossary reads a JSON object of source → target words.
func LoadGlossary(path string) (*Glossary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read glossary: %w", err)
	}
	var pairs map[string]string
	if err := json.Unmarshal(raw, &pairs); err != nil {
		return nil, fmt.Errorf("parse glossary %s: %w", path, err)
	}
	return NewGlossary(pairs)
}

// Translate replaces every word found in the glossary.
func (g *Glossary) Translate(normalizedName string) string {
	words := strings.Fields(normalizedName)
	for i, w := range words {
		if t, ok := g.words[w]; ok {
			words[i] = t
		}
	}
	return strings.Join(words, " ")
}

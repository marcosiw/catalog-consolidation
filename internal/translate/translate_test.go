package translate

import (
	"path/filepath"
	"testing"
)

func TestGlossaryTranslate(t *testing.T) {
	g, err := NewGlossary(map[string]string{"Roteador": "Router", "processador": "processor"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"finding 9: router", "roteador wifi 6 tp link", "router wifi 6 tp link"},
		{"finding 9: processor", "processador amd ryzen 9 7950x", "processor amd ryzen 9 7950x"},
		{"nothing to translate", "router wifi 6 tp link", "router wifi 6 tp link"},
		{"model variants are untouched", "galaxy s24 ultra", "galaxy s24 ultra"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.Translate(tt.in); got != tt.want {
				t.Errorf("Translate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewGlossaryRejectsMultiWordSource(t *testing.T) {
	if _, err := NewGlossary(map[string]string{"fone de ouvido": "headphones"}); err == nil {
		t.Error("want error for a multi-word source")
	}
}

func TestShippedGlossaryLoads(t *testing.T) {
	g, err := LoadGlossary(filepath.Join("..", "..", "glossary", "pt-en.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Translate("roteador wifi 6 tp link"); got != "router wifi 6 tp link" {
		t.Errorf("shipped glossary: got %q", got)
	}
}

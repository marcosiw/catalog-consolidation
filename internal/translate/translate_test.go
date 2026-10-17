package translate

import (
	"path/filepath"
	"testing"
)

func TestGlossary_Translate(t *testing.T) {
	g, err := NewGlossary(map[string]string{"Roteador": "Router", "processador": "processor"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"translates_portuguese_first_word", "roteador wifi 6 tp link", "router wifi 6 tp link"},
		{"translates_another_portuguese_first_word", "processador amd ryzen 9 7950x", "processor amd ryzen 9 7950x"},
		{"keeps_english_name_unchanged", "router wifi 6 tp link", "router wifi 6 tp link"},
		{"keeps_model_variant_unchanged", "galaxy s24 ultra", "galaxy s24 ultra"},
		{"keeps_empty_input_empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.Translate(tt.in); got != tt.want {
				t.Errorf("Translate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewGlossary_RejectsMultiWordSource(t *testing.T) {
	if _, err := NewGlossary(map[string]string{"fone de ouvido": "headphones"}); err == nil {
		t.Error("want error for a multi-word source")
	}
}

func TestLoadGlossary_ShippedFileTranslatesRouter(t *testing.T) {
	g, err := LoadGlossary(filepath.Join("..", "..", "glossary", "pt-en.json"))
	if err != nil {
		t.Fatal(err)
	}
	in, want := "roteador wifi 6 tp link", "router wifi 6 tp link"
	if got := g.Translate(in); got != want {
		t.Errorf("Translate(%q) = %q, want %q", in, got, want)
	}
}

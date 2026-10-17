package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestScan(t *testing.T) {
	t.Run("streams_entries_in_file_order_with_positions", func(t *testing.T) {
		data := `[{"Id":"a","SellerName":"S","Name":"X"},{"Id":"b","SellerName":"S","Name":"Y","Brand":null}]`
		var got []Entry
		if _, err := scan(Bytes("f", []byte(data)), func(e Entry) error { got = append(got, e); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Position != 1 || got[1].ID != "b" || got[1].Brand != nil {
			t.Errorf("scan = %+v, want entries a and b at positions 1 and 2", got)
		}
	})
	t.Run("hashes_the_whole_file", func(t *testing.T) {
		data := []byte("[\n  {\"Id\":\"a\",\"SellerName\":\"S\",\"Name\":\"X\"}\n]\n\n")
		sum, err := scan(Bytes("f", data), func(Entry) error { return nil })
		want := sha256.Sum256(data)
		if err != nil || sum != hex.EncodeToString(want[:]) {
			t.Errorf("scan hash = (%s, %v), want %x", sum, err, want)
		}
	})
	for name, data := range map[string]string{
		"rejects_object_instead_of_array": `{"Id":"a"}`,
		"rejects_truncated_array":         `[{"Id":"a","SellerName":"S","Name":"X"}`,
		"rejects_malformed_entry":         `[{"Id":"a"},{"Id":}]`,
		"rejects_empty_file":              ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := scan(Bytes("f", []byte(data)), func(Entry) error { return nil }); err == nil {
				t.Errorf("scan(%q) succeeded, want an error", data)
			}
		})
	}
}

func TestImporter_Run_RefusesMalformedFileBeforeWritingAnything(t *testing.T) {
	f := newFixture(t, catalog...)
	im, err := New(f.store, defaultConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := im.Run(context.Background(), Bytes("entries.json", []byte(`[{"Id":"a1","SellerName":"S","Name":"Xbox Series X","Brand":"Microsoft"},{"Id":`)))
	if err == nil {
		t.Fatal("Run succeeded, want a parse error")
	}
	if rep.RunID != 0 || f.count(t, `SELECT count(*) FROM ImportRun`) != 0 || f.count(t, `SELECT count(*) FROM SellerProduct`) != 0 {
		t.Errorf("run %d recorded or links written, want nothing written", rep.RunID)
	}
}

// changingSource returns other content from the second Open on, as a file
// rewritten while it is being imported.
type changingSource struct {
	opens        int
	first, later []byte
}

func (c *changingSource) Name() string { return "entries.json" }
func (c *changingSource) Open() (io.ReadCloser, error) {
	c.opens++
	if c.opens == 1 {
		return io.NopCloser(bytes.NewReader(c.first)), nil
	}
	return io.NopCloser(bytes.NewReader(c.later)), nil
}

func TestImporter_Run_FailsWhenFileChangesBetweenPasses(t *testing.T) {
	f := newFixture(t, catalog...)
	im, err := New(f.store, defaultConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	src := &changingSource{
		first: entriesJSON(t, in{"a1", "S1", "Xbox Series X", "Microsoft", nil}),
		later: entriesJSON(t, in{"a1", "S1", "Camera Canon EOS R6", "Canon", nil}),
	}
	rep, err := im.Run(context.Background(), src)
	if !errors.Is(err, errSourceChanged) {
		t.Fatalf("Run error = %v, want %v", err, errSourceChanged)
	}
	var status string
	f.db.QueryRow(`SELECT Status FROM ImportRun WHERE Id = ?`, rep.RunID).Scan(&status)
	if !strings.EqualFold(status, "failed") {
		t.Errorf("ImportRun status = %q, want failed", status)
	}
}

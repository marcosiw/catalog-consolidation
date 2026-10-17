package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/marcosiw/catalog-consolidation/internal/store"
	"github.com/marcosiw/catalog-consolidation/internal/translate"
)

func TestImporter_Run_RecordsCompletedRun(t *testing.T) {
	f := newFixture(t, catalog...)
	rep := f.run(t, defaultConfig(t), in{"a1", "GameStore", "Xbox Series X", "Microsoft", nil})
	var status string
	var linked int
	if err := f.db.QueryRow(`SELECT Status, Linked FROM ImportRun WHERE Id = ?`, rep.RunID).Scan(&status, &linked); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || linked != 1 {
		t.Errorf("ImportRun = (%s, %d), want (completed, 1)", status, linked)
	}
}

func TestImporter_Run_GivesSameResultWithSmallBatches(t *testing.T) {
	entries := []in{
		{"a1", "S1", "Xbox Series X", "Microsoft", nil},
		{"a2", "S1", "Nintendo Switch OLED", "Nintendo", nil},
		{"a1", "S2", "Nintendo  Switch OLED", "Nintendo", nil},
		{"a3", "S1", "Roteador WiFi 6 TP-Link", "TP-Link", nil},
		{"a2", "S1", "Nintendo Switch OLED", "Nintendo", "Gaming"},
	}
	big := newFixture(t, catalog...)
	small := newFixture(t, catalog...)
	cfg := defaultConfig(t)
	want := big.run(t, cfg, entries...).Counters
	cfg.BatchSize = 1
	got := small.run(t, cfg, entries...).Counters
	if got != want {
		t.Errorf("counters with batch size 1 = %+v, want %+v", got, want)
	}
}

// cancellingTranslator cancels the import the first time it is used, which
// makes the next database call of that batch fail.
type cancellingTranslator struct {
	translate.Translator
	cancel context.CancelFunc
}

func (c cancellingTranslator) Translate(name string) string {
	c.cancel()
	return c.Translator.Translate(name)
}

func TestImporter_Run_KeepsCommittedBatchesWhenALaterBatchFails(t *testing.T) {
	f := newFixture(t, catalog...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := defaultConfig(t)
	cfg.BatchSize = 1
	cfg.Translator = cancellingTranslator{cfg.Translator, cancel}
	im, err := New(f.store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	data := entriesJSON(t,
		in{"a1", "S1", "Xbox Series X", "Microsoft", nil},
		in{"a2", "S1", "Roteador WiFi 6 TP-Link", "TP-Link", nil}, // reaches translation
	)
	rep, err := im.Run(ctx, Bytes("entries.json", data))
	if err == nil {
		t.Fatal("Run succeeded, want the cancelled batch to fail")
	}
	if rep.Status != store.RunFailed || rep.Counters.Linked != 1 {
		t.Errorf("report = (%s, %+v), want failed with the first batch counted", rep.Status, rep.Counters)
	}
	if n := f.count(t, `SELECT count(*) FROM SellerProduct`); n != 1 {
		t.Errorf("links = %d, want only the committed batch", n)
	}
	var status string
	f.db.QueryRow(`SELECT Status FROM ImportRun WHERE Id = ?`, rep.RunID).Scan(&status)
	if !strings.EqualFold(status, "failed") {
		t.Errorf("ImportRun status = %q, want failed", status)
	}
}

type panickingTranslator struct{}

func (panickingTranslator) Translate(string) string { panic("translator bug") }

func TestImporter_Run_MarksRunFailedAndRepanicsOnPanic(t *testing.T) {
	f := newFixture(t, catalog...)
	cfg := defaultConfig(t)
	cfg.Translator = panickingTranslator{}
	im, err := New(f.store, cfg)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("Run did not propagate the panic")
			}
		}()
		im.Run(context.Background(), Bytes("entries.json", entriesJSON(t,
			in{"a1", "S1", "Roteador WiFi 6 TP-Link", "TP-Link", nil}, // reaches translation
		)))
	}()
	var status string
	if err := f.db.QueryRow(`SELECT Status FROM ImportRun`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Errorf("ImportRun status = %q, want failed", status)
	}
}

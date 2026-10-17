package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcosiw/catalog-consolidation/internal/report"
	"github.com/marcosiw/catalog-consolidation/internal/storetest"
)

// The integration tests run the command end to end on a versioned fixture:
// a database with the original schema (so the migration runs) and a
// two-entry file in testdata/. One entry matches a seeded product after
// normalization, the other becomes a new product.

type cli struct {
	db, reports string
}

func newCLI(t *testing.T) cli {
	t.Helper()
	return cli{
		db: storetest.NewOriginalDB(t,
			storetest.P("Xbox Series X", "Microsoft", "Gaming"),
			storetest.P("PlayStation 5 Console", "Sony", "Gaming"),
		),
		reports: t.TempDir(),
	}
}

func (c cli) run(t *testing.T, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{
		"-db", c.db,
		"-input", filepath.Join("testdata", "entries.json"),
		"-glossary", filepath.Join("..", "..", "glossary", "pt-en.json"),
		"-exclusion-list", filepath.Join("..", "..", "rules", "exclusion-list.json"),
		"-report-dir", c.reports,
	}, extra...)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func (c cli) count(t *testing.T, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+c.db)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func readReport(t *testing.T, path string) report.Report {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRun_ImportsFixtureAndIsIdempotent(t *testing.T) {
	c := newCLI(t)

	code, stdout, stderr := c.run(t)
	if code != exitOK {
		t.Fatalf("first run exit = %d, stderr = %s", code, stderr)
	}
	first := readReport(t, filepath.Join(c.reports, "run-1.json"))
	if first.Counters.Linked != 1 || first.Counters.Created != 1 {
		t.Errorf("first run counters = %+v, want 1 linked and 1 created", first.Counters)
	}
	if !strings.Contains(stdout, "report: ") {
		t.Errorf("stdout does not show the report path:\n%s", stdout)
	}
	if p, l := c.count(t, "Product"), c.count(t, "SellerProduct"); p != 3 || l != 2 {
		t.Errorf("after first run: %d products and %d links, want 3 and 2", p, l)
	}

	code, _, stderr = c.run(t)
	if code != exitOK {
		t.Fatalf("second run exit = %d, stderr = %s", code, stderr)
	}
	second := readReport(t, filepath.Join(c.reports, "run-2.json"))
	if second.Counters.Unchanged != 2 || second.Counters.Created+second.Counters.Linked+second.Counters.Moved != 0 {
		t.Errorf("second run counters = %+v, want 2 unchanged and nothing else", second.Counters)
	}
	if p, l := c.count(t, "Product"), c.count(t, "SellerProduct"); p != 3 || l != 2 {
		t.Errorf("after second run: %d products and %d links, want 3 and 2", p, l)
	}
}

func TestRun_DryRunWritesReportAndChangesNothing(t *testing.T) {
	c := newCLI(t)
	code, _, stderr := c.run(t, "-dry-run")
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(c.reports, "dry-run-*.json"))
	if len(files) != 1 {
		t.Fatalf("dry-run reports = %v, want one", files)
	}
	if r := readReport(t, files[0]); !r.DryRun || r.Counters.Created != 1 {
		t.Errorf("dry-run report = %+v, want dry_run with 1 created", r.Counters)
	}
	if p, l, runs := c.count(t, "Product"), c.count(t, "SellerProduct"), c.count(t, "ImportRun"); p != 2 || l != 0 || runs != 0 {
		t.Errorf("after dry run: %d products, %d links, %d runs; want 2, 0, 0", p, l, runs)
	}
}

func TestRun_ReturnsUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"rejects_unknown_flag", []string{"-unknown"}},
		{"rejects_positional_arguments", []string{"extra"}},
		{"rejects_threshold_out_of_range", []string{"-similarity-threshold", "1.5"}},
		{"rejects_missing_input_file", []string{"-input", "does-not-exist.json"}},
		{"rejects_missing_glossary", []string{"-glossary", "does-not-exist.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCLI(t)
			if code, _, _ := c.run(t, tt.args...); code != exitUsage {
				t.Errorf("exit = %d, want %d", code, exitUsage)
			}
		})
	}
}

func TestRun_ShowsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"-h"}, &stdout, &stderr); code != exitOK {
		t.Errorf("exit = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stderr.String(), "-similarity-threshold") {
		t.Errorf("help does not list the flags:\n%s", stderr.String())
	}
}

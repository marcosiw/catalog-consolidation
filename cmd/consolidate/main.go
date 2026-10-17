// Command consolidate imports a seller file into the catalog database.
//
// Usage and flags are described in SPEC.md section 6. Exit codes:
//
//	0  the import completed
//	1  the import failed (the report shows what was committed)
//	2  invalid usage or configuration
//	3  internal error: a panic, reported with its stack trace
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/marcosiw/catalog-consolidation/internal/ingest"
	"github.com/marcosiw/catalog-consolidation/internal/rules"
	"github.com/marcosiw/catalog-consolidation/internal/store"
	"github.com/marcosiw/catalog-consolidation/internal/translate"
)

const (
	exitOK       = 0
	exitFailed   = 1
	exitUsage    = 2
	exitInternal = 3
)

func main() {
	// Ctrl+C or SIGTERM cancel the import; the batch in progress is rolled
	// back and the run is recorded as failed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is main without the process: testable, and the place where panics are
// recovered, since os.Exit in main would skip deferred calls.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		if p := recover(); p != nil {
			fmt.Fprintf(stderr, "internal error: %v\n%s", p, debug.Stack())
			code = exitInternal
		}
	}()

	cfg, err := parseFlags(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}

	glossary, err := translate.LoadGlossary(cfg.glossary)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	exclusions, err := rules.LoadExclusionList(cfg.exclusionList)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	data, err := os.ReadFile(cfg.input)
	if err != nil {
		fmt.Fprintf(stderr, "read input: %v\n", err)
		return exitUsage
	}

	s, err := store.Open(ctx, cfg.db)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailed
	}
	defer s.Close()

	ingestion, err := ingest.New(s, ingest.Config{
		SimilarityThreshold: cfg.threshold,
		BatchSize:           cfg.batchSize,
		DryRun:              cfg.dryRun,
		Translator:          glossary,
		Exclusions:          exclusions,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}

	rep, runErr := ingestion.Run(ctx, cfg.input, data)
	// The report is written even when the run failed: it shows what the
	// committed batches did.
	rep.WriteSummary(stdout)
	path, err := rep.WriteFile(cfg.reportDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailed
	}
	fmt.Fprintf(stdout, "report: %s\n", path)
	if runErr != nil {
		fmt.Fprintf(stderr, "import failed: %v\n", runErr)
		return exitFailed
	}
	return exitOK
}

type config struct {
	db, input, glossary, exclusionList, reportDir string
	threshold                                     float64
	batchSize                                     int
	dryRun                                        bool
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	var c config
	fs := flag.NewFlagSet("consolidate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.db, "db", "data/catalog.db", "SQLite database to import into; migrated automatically on first use")
	fs.StringVar(&c.input, "input", "data/ProductEntry.json", "JSON array of seller entries")
	fs.Float64Var(&c.threshold, "similarity-threshold", 0.6, "minimum token Jaccard score for a same-brand candidate, in (0, 1]")
	fs.StringVar(&c.glossary, "glossary", "glossary/pt-en.json", "word-by-word translation glossary that confirms candidates")
	fs.StringVar(&c.exclusionList, "exclusion-list", "rules/exclusion-list.json", "terms that reject an entry when contained in its name or brand")
	fs.StringVar(&c.reportDir, "report-dir", "reports", "directory where the JSON report is written")
	fs.IntVar(&c.batchSize, "batch-size", 500, "entries committed per transaction; ignored with -dry-run")
	fs.BoolVar(&c.dryRun, "dry-run", false, "roll everything back at the end; the report is still written")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() > 0 {
		return config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return c, nil
}

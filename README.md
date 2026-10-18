# Catalog Consolidation

A command-line tool that imports products sent by marketplace sellers into a SQLite catalog **without duplicating products**: an item that already exists in the catalog only gets a link to the seller; an unknown item becomes a new product. Every decision is reported, and every doubtful case is flagged instead of silently decided.

**Start with [`SPEC.md`](SPEC.md).** It is the statement of intent, written before the code: the problem, the 16 findings in the data, the pipeline, each decision with its evidence, the assumptions made and the questions I would ask.

## Quick start

Requirements: Go 1.27 (any Go from 1.21 on downloads it automatically), `make`, `curl`, `sha256sum`, and `sqlite3` for `make inspect`.

```bash
make            # list the targets
make data       # download the provided database and seller file, verify checksums
make test       # go vet + every test (no network or data needed)
make run        # import
make inspect    # look at the result
make run        # again: everything is "unchanged" (idempotency)
make reset      # download the database again, back to its original state
```

Each target is described in [`SPEC.md` section 6](SPEC.md#6-interface), together with the full `go run ./cmd/consolidate` command and every flag.

## Result on the provided data

| Outcome | Entries | |
|---|---|---|
| linked | 267 | 265 by normalized name and brand, 2 by translation (`Roteador…` → `Router…`, `Processador…` → `Processor…`) |
| created | 0 | every legitimate item already existed in the catalog |
| rejected | 1 | `Security Test Product`, by the exclusion list |
| duplicate_in_batch | 1 | `Câmera Canon EOS R6`, superseded by a later message of the same seller and id |
| warnings | 11 | `same_product_multiple_ids`: a seller links the same product with two ids |

A second run of the same file reports 267 `unchanged` and writes nothing. Its warnings rise to 22 and stay there on later runs: the warning is raised per entry, and both entries of each pair now find the other one already linked, while in the first run only the later one does. These numbers are pinned by a test that runs when the data is present.

## How it works

```
0a. validate    empty Id, SellerName or Name?                             yes → reject
0b. distinct    by (SellerName, Id) over the whole file, keeping the LAST occurrence
0c. exclusion   Name or Brand contains a term of the exclusion list?      yes → reject
1.  normalize   (name, brand) normalized key exists in the catalog?       yes → link (normalized)
2.  similarity  same-brand candidates with token Jaccard ≥ 0.6?           no  → new product
3.  translate   translated name equals one candidate exactly?             yes → link (translated)
                                                                          no  → new product + possible_duplicate warning
```

The guiding principle is that **wrongly merging two products is worse than duplicating one**: a duplicate can be merged later, but an offer on the wrong product sells the wrong item. So similarity never links on its own; two independent signals must agree.

The main decisions, detailed in [`SPEC.md` section 4](SPEC.md#4-decisions):

- **Identity of a product** is name + brand, normalized (accents, case, spaces, quotes and apostrophes). Category is not part of it: sellers use their own taxonomies.
- **Identity of a seller item** is `(SellerName, SellerProductId)`. The id is the seller's SKU, an opaque string: no format is checked.
- **The last message wins**, within the file and across runs. A re-sent id pointing to another product moves the link; a product left without sellers is reported, never deleted.
- **The catalog is never changed by seller data.** The link keeps what the seller sent and how it was matched.
- **The database enforces the rules**: `UNIQUE` on the normalized key and on the seller item, foreign keys on, versioned migration (`PRAGMA user_version`).
- **Security** comes from parameterized statements only. The exclusion list is content policy, not an injection filter.
- **Batches bound the transaction, streaming bounds the memory.** Each transaction holds 100 entries by default. The file is streamed twice and never loaded whole: a first pass indexes the last occurrence of each seller item, a second pass imports. Every run is recorded in `ImportRun` with the file's SHA-256; a failed run is recovered by running the file again.

Each run writes a JSON report to `reports/` with every entry's outcome, reason and warnings.

## Project layout

```
cmd/consolidate/       command line: flags, signals, panic recovery, exit codes
internal/normalize/    comparison key (pure)
internal/similarity/   token Jaccard (pure)
internal/translate/    Translator interface and word glossary
internal/rules/        exclusion list
internal/store/        SQLite access and migrations
internal/ingest/       the pipeline: two-pass streaming, validation, distinct, matching, links, batches
internal/report/       report model, JSON file and summary
internal/testutil/     test helper: databases with the original schema
config/                files editable without code changes: glossary, exclusion list, data checksums
```

## Tests

- **Unit tests** cover every finding of `SPEC.md` section 2.
- **Integration tests** run the command end to end, twice, on a versioned two-entry fixture (`cmd/consolidate/testdata/`) against a database built with the original schema, so the migration runs too.
- **Provided-data tests** run the migration and the import on the real files when `make data` was run, and pin the numbers above.
- Failure paths are tested too: a batch cancelled midway, a panic inside the pipeline, invalid usage.

## Conventions

The Guideline Document referenced by the assessment defines no code conventions, so this project follows standard Go practice plus the rules in [`AGENTS.md`](AGENTS.md):

- `gofmt`, `go vet`, `cmd/` + `internal/` layout, table-driven tests, errors wrapped with `%w`.
- **Test names describe behaviour** (`TestText/deletes_apostrophe_instead_of_spacing_it`), in `snake_case` so a failure can be found with `grep`.
- **Commits** follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/). The history tells the story: the SPEC first, then one slice per commit with its tests.

### Versioning

Releases follow [Semantic Versioning](https://semver.org/) with Go module tags. For a command-line tool the public API is not code but:

- the command-line flags;
- the JSON report format;
- the database schema (`PRAGMA user_version`).

Breaking any of them is a major change. The delivered version is `v0.1.0`; while on `0.x`, the API is not considered stable.

## How AI was used

I worked with an AI coding agent (Claude Code) as a pair, spec first. The agent's instructions are in [`AGENTS.md`](AGENTS.md), the single source for any agent: Codex and Cursor read it natively, and `CLAUDE.md`, `GEMINI.md` and `.github/copilot-instructions.md` point to it.

**What the agent did:** explored the data with SQL and scripts, and measured the cases that drive the decisions. One example is the similarity of every same-brand pair in the catalog, which set the threshold and ruled out character-level similarity. It also laid out options and trade-offs for each ambiguity, wrote `SPEC.md` from the decisions taken, and implemented each slice with its tests.

**What I decided:** every entry of `SPEC.md` section 4, one at a time. Several of them changed or replaced the agent's first proposal:

| Agent's first proposal | What I decided, and why |
|---|---|
| Similarity, then translation, as alternative ways to match | Both must agree. A same-brand candidate is only linked if the translation matches it exactly, so `Galaxy S24 Ultra` and `Galaxy S24 Plus` are never merged. |
| First message wins for a re-sent seller id | Last wins. A re-sent id is most likely the seller correcting a mistake. |
| Accept non-UUID ids with a warning | No format check at all. Seller SKUs come in many formats, and the assessment requires none. |
| A per-line import log table | Only `ImportRun`, with the file's hash. To audit or re-process, the file is run again. |
| One transaction per file | Batches of 100. This is still safe thanks to idempotency and last-wins. |
| Batches, with the whole file read into memory | Streaming in two passes, so that the batches are not contradicted by the memory use: batches bound the transaction, streaming bounds the memory. |
| A `--dry-run` mode (one transaction rolled back at the end) | Removed. It is not required, the database is disposable (`make reset`), and it needed a second execution path that was not the one that runs for real. |
| Keep a pristine copy of the database for `reset` | `reset` downloads it again: simpler. |
| No content filter (injection handled by parameterization only) | An exclusion list of known unwanted terms as a separate stage, framed as content policy. |
| Test names prefixed with the finding number | Names that describe behaviour. |
| — | Panic recovery in the command, `make` as the main entry point, Conventional Commits, SemVer. |

**How the output was validated:**

- Each slice was presented with its tests and design notes and approved before the next one started.
- Every finding has a test, and the numbers on the real data are pinned.
- The tests caught a real bug in generated code: when a run was cancelled, the "failed" status was written with the cancelled context, and the run stayed `running`. It is fixed, and the test stays.

## Limitations and future work

These are also listed in [`SPEC.md` section 8](SPEC.md#8-out-of-scope--future-work).

- **Translation is a small PT→EN glossary.** Ambiguous words are left out on purpose (*tênis* means both sneakers and tennis). An LLM or a translation API can be plugged in behind the `Translator` interface.
- **The similarity threshold** was calibrated on only two positive cases. Token Jaccard does not catch typos inside a word. A combined metric is the next step.
- **`possible_duplicate` warnings** have no review queue yet.
- **Sellers are identified by free-text names.** A `Seller` table would fix that.
- **The report is kept in memory** until the end, one line per entry, and the index keeps one key per seller item. For millions of entries, the next steps are a streamed report and a staging table (see `SPEC.md` section 8).

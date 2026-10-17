# SPEC — Catalog Consolidation

This document is the statement of intent for this solution: the problem as I understand it, the assumptions I made, every decision taken on the ambiguities found in the data, and what is deliberately out of scope. It was written **before** the code, and the code follows it. Numbers quoted here were measured on the provided data (`catalog.db` and `ProductEntry.json`).

## 1. Problem

A marketplace catalog holds one canonical record per product (`Product`). Sellers send their own product lists, each item identified by the seller's own id (their SKU). For every incoming item we must answer one question: **does this product already exist in the catalog?**

- **Yes** → do not create a new product; only record that this seller sells it (`SellerProduct`).
- **No** → create the product and record the link.

The shopper must see each product **once**, with every seller's offer attached to it. That is what makes price comparison, a single product page, aggregated reviews and clean search possible.

### The two possible errors are not equally bad

| Error | Effect | Reversible? |
|---|---|---|
| **False negative** (a duplicate is not recognized) | Two pages for the same product; offers and reviews split | Yes — products can be merged later |
| **False positive** (different products are merged) | A seller's offer shows on the wrong product; the shopper buys one thing and receives another | Hard — once orders exist |

**Guiding principle: when in doubt, do not merge.** Every matching rule below trades recall for precision, and every doubtful case is reported instead of silently decided.

## 2. The data

- `Product`: 975 rows, no duplicates (not even after normalization). 119 with `Brand` NULL, 34 with `Category` NULL.
- `SellerProduct`: empty.
- `ProductEntry.json`: 269 entries from 20 sellers, fields `Id, SellerName, Name, Brand, Category`.

### Ambiguities found (each one has a decision in section 4)

| # | Finding | Evidence |
|---|---|---|
| 1 | `SellerProductId` is `INTEGER`, seller ids are UUID strings | schema |
| 2 | 3 ids are not valid UUIDs | `ddddeee-…`, `09835342345-…`, `uddd0000-…` |
| 3 | The same id is used by different sellers for unrelated products | 13 ids |
| 4 | The same seller sends the same id twice with different spellings | GardenStore `e5e5e5e5-…`: "Câmera Canon EOS R6" / "Camera Canon EOS R6" |
| 5 | Double spaces in names | 60 entries, e.g. `Xbox  Series X` |
| 6 | Inch marks vary: `"`, `''` or missing | `iPad Pro 12.9"` vs `12.9''` vs `12.9` |
| 7 | Brand casing is inconsistent **inside the catalog itself** | `Black+Decker` (607) vs `BLACK+DECKER` (661, 663) |
| 8 | Category differs for the same product | `Photo` vs `Photography` |
| 9 | Names partially in Portuguese | `Roteador WiFi 6 TP-Link`, `Processador AMD Ryzen 9 7950X` |
| 10 | NULL brand on both sides — `NULL = NULL` is not true in SQL | 3 entries |
| 11 | SQL injection attempt in a brand | `TestBrand'; SELECT 1; --` |
| 12 | The same file may be processed more than once | — |
| 13 | The guideline asks to follow code conventions but defines none | — |
| 14 | Apostrophe differences | `Levi's` vs `Levis` |
| 15 | Same seller, same product, two different ids | 11 cases |
| 16 | The FK is declared but SQLite only enforces it with `PRAGMA foreign_keys = ON` | schema |

### Matching funnel on the real data

| Rule | Entries matched |
|---|---|
| Exact name and brand with `=` | 198 |
| … with `IS` (NULL-safe) | 201 |
| … also requiring the same category | 200 (worse) |
| Normalized name and brand | 266 |
| Unmatched | 3 (2 translations + 1 test product) |

## 3. Pipeline

```
0.  distinct    by (SellerName, Id) over the whole file, keeping the LAST occurrence
0b. exclusion   Name or Brand contains a term of the exclusion list?      yes → reject (exclusion_list)
1.  normalize   (NormalizedName, NormalizedBrand) exists in the catalog?   yes → link   (normalized)
2.  similarity  same-brand candidates with token Jaccard ≥ threshold?      no  → new product
3.  translate   translated name == NormalizedName of one of them?          yes → link   (translated)
                                                                           no  → new product + possible_duplicate warning
```

- **Normalization** (both sides): Unicode NFKD, drop combining marks (accents), lower case, remove `'` and `"` (so `Levi's` → `levis` and `12.9"` → `12.9`), every other non-alphanumeric run becomes one space, trim. A NULL brand normalizes to `''`.
- **The duplicate key is name + brand. Category is not part of it**: it is classification, not identity, and each seller has its own taxonomy (finding 8).
- **Similarity** never decides a link on its own. It selects candidates (blocking by brand) and flags the grey zone. It is skipped when the brand is empty: the 119 unbranded products are too heterogeneous to be a block.
- **Translation** confirms a candidate only by **exact** equality. Two independent signals must agree. This is what protects against merging `Galaxy S24 Ultra` with `Galaxy S24 Plus`: similar, same brand, but no translation makes them equal.
- **Products created during a run** are visible to the following entries of the same run.

## 4. Decisions

1. **`SellerProductId` becomes `TEXT`** (SQLite has no `ALTER COLUMN`: the table is rebuilt; it is empty). **`UNIQUE(SellerName, SellerProductId)`**: ids are only unique per seller (finding 3). A `Seller` table is future work.
2. **Re-sent `(SellerName, Id)`: last wins.**
   - In the file: the distinct keeps the last occurrence; the others are reported as `duplicate_in_batch` with their line numbers.
   - Across runs: an existing pair goes through the pipeline again. Same product → nothing changes (idempotency). Another product → the link is **moved**, assuming the seller is correcting a previous mistake. A product left without links by the move is reported as **orphaned** and never deleted automatically.
   - Accepted risk: if the new message is the wrong one, the offer moves to the wrong product. Mitigation: re-processing the previous file moves it back (`ImportRun` records which file, with its hash).
   - Future work: update in place a product created by this seller and used only by it.
3. **Ids are opaque.** Non-UUID ids are accepted with an `invalid_id_format` warning; only an empty id is rejected (`missing_id`). The id is the seller's SKU, the platform never interprets it, and external ids come in many formats.
4. **Same seller, same product, different ids: both links are kept**, with a `same_product_multiple_ids` warning. A partly-1P/partly-3P store may sell the same product as own stock and consignment through the same seller account. The catalog stays duplicate-free; only the offer repeats, and that is reversible. Future work: an offer discriminator column (condition, fulfillment).
5. **`SellerProduct` keeps the seller's last message and how the link was made:** `SellerProductName`, `SellerBrand`, `SellerCategory`, `MatchMethod` (`normalized` | `translated` | `new`), `MatchScore`, `CreatedAt`, `UpdatedAt`, `ImportRunId`. **The catalog is never changed by seller data.** History is one table, `ImportRun` (file path, SHA-256, timestamps, status, counters): to audit or re-process, the file is run again. No per-line log is persisted.
6. **`Product` gets `NormalizedName` and `NormalizedBrand`**, backfilled by the migration, with `UNIQUE(NormalizedName, NormalizedBrand)` and an index on `NormalizedBrand`. The database itself refuses catalog duplicates, even with concurrent imports. The brand is `''`, not NULL, because NULLs never collide in a SQLite `UNIQUE`. If the normalization rule changes, the same function re-runs the backfill.
7. **Similarity is token Jaccard, threshold 0.6, configurable.** Measured: the two translations score 0.67; the most similar pair of *different* same-brand products in the catalog scores 0.50 (`Tennis Racket Adult` × `Tennis Racket Bag`). Character-level similarity was rejected: `Colander Stainless Steel` × `Ladle Stainless Steel` scores 0.89 against 0.91/0.96 for the translations. Caveats: calibrated on only two positives; it does not catch typos inside a word.
8. **The SQL injection entry is ordinary data.** Security lives in the data access layer: **every statement is parameterized**, including migrations. A test proves the brand is stored literally. Filtering characters was rejected: it would break legitimate brands (`Levi's`, `Black+Decker`, `AT&T`) and protects nothing that parameterization does not.
9. **Execution.**
   - Batches of 500 entries per transaction (configurable). The distinct runs on the whole file before batching. `ImportRun` is written first (`running`), counters are updated per committed batch, and it ends as `completed` or `failed`. A failed run is recovered by running the file again — safe because of idempotency and last-wins.
   - Data errors reject the entry and the run goes on (`missing_id`, `missing_name`; an empty brand is allowed). System errors (malformed JSON, database unavailable, unexpected constraint violation) roll back the batch, mark the run `failed` and exit non-zero.
   - Migrations are versioned with `PRAGMA user_version`, idempotent and applied automatically at start. `PRAGMA foreign_keys = ON` on every connection.
   - `--dry-run` runs everything in a single transaction that is rolled back at the end (batch size ignored) and still writes the report.
10. **Exclusion list** (stage 0b): a file of forbidden terms that grows without code changes. If `Name` or `Brand` contains a term anywhere, the entry is rejected (`exclusion_list`, with the matched term). Comparison is case-insensitive with collapsed spaces, **keeping punctuation**. Initial terms: `Security Test Product`, `SELECT 1;`. This is **catalog content policy** (sellers pushing test items to production), not a security control. Terms must be specific: `test` alone would match `Contest`.
11. **Conventions.** No code conventions are defined in the guideline, so this follows standard Go (`gofmt`, `go vet`, `cmd/` + `internal/`, table-driven tests, wrapped errors). Driver `modernc.org/sqlite` (pure Go, no CGO needed).

## 5. Report

Each run prints a summary and writes `reports/run-<id>.json`:

- counters: `created`, `linked`, `moved`, `unchanged`, `rejected`, `duplicate_in_batch`;
- every rejection with line number and reason;
- every warning with line number: `invalid_id_format`, `same_product_multiple_ids`, `possible_duplicate` (candidate id and score), `orphaned_product`.

## 6. Interface

```bash
go run ./cmd/consolidate \
  --db data/catalog.db --input data/ProductEntry.json \
  --similarity-threshold 0.6 --glossary glossary/pt-en.json \
  --exclusion-list rules/exclusion-list.json \
  --report-dir reports --batch-size 500 [--dry-run]
```

`make data` (download + checksum), `make test`, `make dry-run`, `make run`, `make reset`, `make inspect`.

## 7. Tests

- **Unit**: one test per finding of section 2.
- **Integration** with a versioned fixture (`testdata/`): a database built from the original DDL plus a small seed (so the migration is exercised), and a 2-entry file — one matches an existing product, one becomes a new product. Run twice: the second run changes nothing.
- **Real data** (skipped when `data/` is absent): checks the funnel numbers.

## 8. Out of scope / future work

- LLM or translation API behind the `Translator` interface (today: a small PT→EN glossary).
- A review queue for `possible_duplicate`.
- Combined similarity metric (tokens + characters) to catch typos.
- `Seller` table; offer discriminator.
- Per-line import log.
- Allow-list and presence rules (e.g. "category X requires brand").

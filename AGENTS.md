# Agent instructions

Read `SPEC.md` before any change. It is the source of truth; if a change contradicts it, update the SPEC first and say why.

## Rules

- Never build SQL by concatenating data. Every statement uses `?` parameters, including migrations and tests.
- Every new behaviour comes with a test. Each finding in `SPEC.md` section 2 has a test that names it.
- Never merge products on a single signal. Matching rules must follow `SPEC.md` section 3.
- The catalog (`Product`) is never updated from seller data; only `SellerProduct` stores what the seller sent.
- Keep functions in `internal/normalize` and `internal/similarity` pure.
- Do not commit `data/*.db`, `data/*.json` or `reports/`.
- Do not add the assessment statement or any material from the hiring process to the repository.

## Conventions

- Standard Go: `gofmt`, `go vet ./...`, `cmd/` + `internal/` layout, table-driven tests, errors wrapped with `%w`.
- SQLite driver: `modernc.org/sqlite` (no CGO).
- English for code, comments, docs and commit messages.
- Small commits, one slice of the SPEC each, tests included.

## Commands

```bash
make test      # unit + integration tests
make data      # download the real data and verify checksums
make dry-run   # simulate an import, write the report, change nothing
make run       # import
make reset     # restore the original database
make inspect   # query the result
```

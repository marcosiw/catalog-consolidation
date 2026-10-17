# Agent instructions

Read `SPEC.md` before any change. It is the source of truth; if a change contradicts it, update the SPEC first and say why.

## Rules

- Never build SQL by concatenating data. Every statement uses `?` parameters, including migrations and tests.
- Every new behaviour comes with a test. Every finding in `SPEC.md` section 2 is covered by at least one test.
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

## Test naming

Test names describe the expected behaviour, never the problem that motivated it, so a failure reads as a sentence and can be found by copying it.

- Test functions: `Test<Func>`, `Test<Type>_<Method>`, or `Test<Func>_<Scenario>` for a specific scenario (`TestText`, `TestGlossary_Translate`, `TestJaccard_DefaultThresholdSeparatesMeasuredCases`).
- Subtests (`t.Run`): `snake_case` verb phrases describing the behaviour (`removes_accents`, `deletes_apostrophe_instead_of_spacing_it`). Go turns spaces into `_` in the output, so writing them in `snake_case` keeps the failure output identical to the source: `grep -rn <name>` finds the case.
- Failure messages: `Func(input) = got, want expected`, got before want.

## Commits and versions

- Commit messages follow [Conventional Commits 1.0](https://www.conventionalcommits.org/en/v1.0.0/): `<type>(<scope>): <summary>`, imperative, lower case, no final period, header up to 72 characters.
  - Types: `feat`, `fix`, `test`, `refactor`, `docs`, `build` (Makefile, dependencies), `chore`.
  - Scope is the package or area: `normalize`, `similarity`, `translate`, `rules`, `store`, `ingest`, `report`, `cli`, `matching` (several matching packages at once).
  - An incompatible change to the public API adds a `BREAKING CHANGE:` footer.
  - Keep the `Co-Authored-By:` trailer when an AI agent took part.
- Releases follow [Semantic Versioning 2.0](https://semver.org/) with Go module tags (`vMAJOR.MINOR.PATCH`). The public API is:
  - the command-line flags;
  - the JSON report format;
  - the database schema (`PRAGMA user_version`).

  Breaking any of them is a major change; `feat` is a minor change and `fix` a patch. While on `0.x` the API is not considered stable. The delivered version is tagged `v0.1.0`.

## Commands

```bash
make test      # unit + integration tests
make data      # download the real data and verify checksums
make run       # import
make reset     # restore the original database
make inspect   # query the result
```

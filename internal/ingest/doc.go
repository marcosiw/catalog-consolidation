// Package ingest consolidates a seller file into the catalog (SPEC section 3).
//
// Files, in the order an import goes through them:
//
//   - importer.go: configuration and construction of an Importer;
//   - source.go: the input, streamed in two passes (index, then import);
//   - entry.go: an entry of the file, its validation and its triage;
//   - run.go: the life cycle of a run: start, batches, completion, failure;
//   - process.go: the pipeline of one entry, from exclusion to warnings;
//   - match.go: which catalog product an entry belongs to (stages 1 to 3);
//   - link.go: the seller's link to that product: new, kept or moved.
package ingest

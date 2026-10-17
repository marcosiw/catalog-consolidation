package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
)

// Source is the input file. It is opened twice: once to index it, once to
// import it, so the file is streamed and never held in memory.
type Source interface {
	Name() string
	Open() (io.ReadCloser, error)
}

// File is a Source read from disk.
type File string

func (f File) Name() string                 { return string(f) }
func (f File) Open() (io.ReadCloser, error) { return os.Open(string(f)) }

// Bytes is a Source held in memory, for tests and small inputs.
func Bytes(name string, data []byte) Source { return bytesSource{name, data} }

type bytesSource struct {
	name string
	data []byte
}

func (b bytesSource) Name() string                 { return b.name }
func (b bytesSource) Open() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b.data)), nil }

// index is what the first pass learns about the file: its hash and, for each
// seller item, the position of its last occurrence. Memory grows with the
// number of distinct (SellerName, Id) keys, not with the size of the entries.
type index struct {
	sha256 string
	last   map[sellerItem]int
}

type sellerItem struct {
	sellerName string
	id         string
}

// indexSource is the first pass. A malformed file fails here, before
// anything is written to the database.
func indexSource(src Source) (index, error) {
	idx := index{last: make(map[sellerItem]int)}
	sum, err := scan(src, func(e Entry) error {
		if validate(e) == "" {
			idx.last[sellerItem{e.SellerName, e.ID}] = e.Position
		}
		return nil
	})
	if err != nil {
		return index{}, err
	}
	idx.sha256 = sum
	return idx, nil
}

// errSourceChanged reports that the file changed between the two passes.
var errSourceChanged = errors.New("input file changed during the import")

// stream is the second pass: it calls fn for every entry, in file order, and
// checks that the file is still the one indexed by the first pass.
func stream(src Source, idx index, fn func(Entry) error) error {
	sum, err := scan(src, fn)
	if err != nil {
		return err
	}
	if sum != idx.sha256 {
		return errSourceChanged
	}
	return nil
}

// scan decodes the JSON array one entry at a time and returns the SHA-256 of
// the whole file.
func scan(src Source, fn func(Entry) error) (string, error) {
	f, err := src.Open()
	if err != nil {
		return "", fmt.Errorf("open input: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	r := io.TeeReader(f, h)
	dec := json.NewDecoder(r)

	if err := expectDelim(dec, '['); err != nil {
		return "", err
	}
	for position := 1; dec.More(); position++ {
		var raw rawEntry
		if err := dec.Decode(&raw); err != nil {
			return "", fmt.Errorf("parse entry at position %d: %w", position, err)
		}
		if err := fn(raw.entry(position)); err != nil {
			return "", err
		}
	}
	if err := expectDelim(dec, ']'); err != nil {
		return "", err
	}
	// The decoder reads ahead in chunks; hash whatever it left unread.
	if _, err := io.Copy(io.Discard, r); err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return hexSum(h), nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("parse entries: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("parse entries: expected %q, found %v", want, tok)
	}
	return nil
}

func hexSum(h hash.Hash) string { return hex.EncodeToString(h.Sum(nil)) }

// Package search searches a snapshot of chunked input without assembling lines.
package search

import (
	"bytes"
	"context"
	"errors"
	"glimpse/internal/content"
	"io"
	"unicode"
)

type Source interface {
	io.ReaderAt
	Len() int64
}

type Result struct {
	Offset  int64
	Found   bool
	Wrapped bool
}

// Find performs a rune-aware search. Origin is a byte offset; include allows a
// match exactly at origin. Smart case folds only queries without any uppercase
// letters; ignoreCase folds every query unless smart case overrides it. Literal
// queries use KMP, which keeps both working memory and read buffers bounded by
// the query size, even on an indefinitely long logical line; regular
// expressions buffer at most a line, or a window of a very long one.
func Find(ctx context.Context, source Source, query Query, origin int64, backward, include bool) (Result, error) {
	return FindFrom(ctx, source, query, origin, sourceStart(source), backward, include)
}

// FindFrom accepts a known-safe logical-line start at or before origin. It tries
// that nearby range first, falling back to the prefix only when necessary. The
// caller must obtain anchor from decoded content, not raw newline byte searches:
// newlines may occur inside hidden OSC/DCS strings.
func FindFrom(ctx context.Context, source Source, query Query, origin, anchor int64, backward, include bool) (Result, error) {
	base := sourceStart(source)
	end := source.Len()
	origin = min(max(base, origin), end)
	anchor = min(max(base, anchor), origin)
	eligible := func(offset int64) bool {
		if backward {
			return offset < origin || include && offset == origin
		}
		return offset > origin || include && offset == origin
	}
	var firstWrapped Result
	scan := func(start, lastStart int64, wantEligible, wrapped bool) (Result, error) {
		var found Result
		err := walk(ctx, source, query, start, end, lastStart, func(offset, end int64) bool {
			if eligible(offset) == wantEligible {
				found = Result{Offset: offset, Found: true, Wrapped: wrapped}
				return backward
			}
			if !backward && wantEligible && !firstWrapped.Found {
				firstWrapped = Result{Offset: offset, Found: true, Wrapped: true}
			}
			return true
		})
		return found, err
	}
	limit := end
	if backward {
		limit = origin
		if !include {
			limit--
		}
	}
	if found, err := scan(anchor, limit, true, false); found.Found || err != nil {
		return found, err
	}
	if !backward {
		if anchor > base {
			if found, err := scan(base, anchor-1, false, true); found.Found || err != nil {
				return found, err
			}
		}
		return firstWrapped, nil
	}
	if anchor > base {
		if found, err := scan(base, min(limit, anchor-1), true, false); found.Found || err != nil {
			return found, err
		}
	}
	return scan(anchor, end, false, true)
}

// Walk streams every match in an absolute byte range: overlapping ones for a
// literal, leftmost non-overlapping ones for a regular expression. start must
// be a line start for ^ to match correctly. Returning false from visit stops
// immediately without retaining matches.
func Walk(ctx context.Context, source Source, query Query, start, end int64, visit func(start, end int64) bool) error {
	return walk(ctx, source, query, start, end, end, visit)
}

func walk(ctx context.Context, source Source, query Query, start, end, lastStart int64, visit func(start, end int64) bool) error {
	_, _, err := walkChecked(ctx, source, query, start, end, lastStart, 0, visit)
	return err
}

// walkChecked also reports whether a decoded token spans boundary (when
// positive). Unless visit stops early, decoding passes lastStart, so a walk with
// lastStart = boundary-1 has examined every token that could span it. open
// reports that decoding reached end before passing lastStart, so matches
// starting through lastStart may change once more input arrives.
func walkChecked(ctx context.Context, source Source, query Query, start, end, lastStart, boundary int64, visit func(start, end int64) bool) (straddled, open bool, err error) {
	m, err := query.compile()
	if err != nil {
		return false, false, err
	}
	end = min(end, source.Len())
	start = min(max(sourceStart(source), start), end)
	reader := searchReader{reader: content.NewPlainReader(source, start, end), ctx: ctx, boundary: boundary}
	if m.re != nil {
		return walkRegex(&reader, m, start, lastStart, visit)
	}
	if len(m.literal) == 0 {
		return false, false, nil
	}
	return walkLiteral(&reader, m.literal, m.fold, lastStart, visit)
}

// walkLiteral runs KMP over the decoded text; pattern is already folded.
func walkLiteral(reader *searchReader, pattern []rune, fold bool, lastStart int64, visit func(start, end int64) bool) (straddled, open bool, err error) {
	ctx := reader.ctx
	prefix := make([]int, len(pattern))
	for i, j := 1, 0; i < len(pattern); i++ {
		for j > 0 && pattern[i] != pattern[j] {
			j = prefix[j-1]
		}
		if pattern[i] == pattern[j] {
			j++
		}
		prefix[i] = j
	}
	offsets := make([]int64, len(pattern))
	index := 0
	matched := 0
	consume := func(r rune, start, end int64) bool {
		// A candidate may extend past lastStart, but once its earliest active
		// prefix starts later there can be no remaining eligible match.
		if start > lastStart {
			prefixStart := start
			if matched > 0 {
				prefixStart = offsets[(index-matched+len(pattern))%len(pattern)]
			}
			if prefixStart > lastStart {
				return false
			}
		}
		offsets[index] = start
		index++
		if index == len(pattern) {
			index = 0
		}
		if fold {
			if r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			} else if r >= 128 {
				r = foldRune(r)
			}
		}
		for matched > 0 && pattern[matched] != r {
			matched = prefix[matched-1]
		}
		if pattern[matched] == r {
			matched++
		}
		if matched != len(pattern) {
			return true
		}
		offset := offsets[index]
		matched = prefix[matched-1]
		if offset > lastStart {
			return false
		}
		return visit(offset, end)
	}
	first := pattern[0]
	firstBytes := string(first)
	if fold && first >= 'A' && first <= 'Z' {
		firstBytes += string(first + 'a' - 'A')
	}
	for {
		if err := ctx.Err(); err != nil {
			return false, false, err
		}
		if run, ok := reader.ascii(); ok {
			if err := ctx.Err(); err != nil {
				return false, false, err
			}
			if run.Start > lastStart && matched == 0 {
				return reader.straddled, false, nil
			}
			data, offset := run.Bytes, run.Start
			for len(data) > 0 {
				if matched == 0 {
					i := -1
					if first < 128 {
						if len(firstBytes) == 1 {
							i = bytes.IndexByte(data, byte(first))
						} else {
							i = bytes.IndexAny(data, firstBytes)
						}
					}
					if i < 0 {
						break
					}
					data, offset = data[i:], offset+int64(i)
				}
				if !consume(rune(data[0]), offset, offset+1) {
					return reader.straddled, false, nil
				}
				data, offset = data[1:], offset+1
			}
			continue
		}
		token, err := reader.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return false, false, err
		}
		// Overstrike normalization can merge tokens across a hidden control.
		reader.check(token.Start, token.End)
		if token.Hidden {
			continue
		}
		if !consume(token.Raw, token.Start, token.End) {
			return reader.straddled, false, nil
		}
	}
	return reader.straddled, true, nil
}

func sourceStart(source Source) int64 {
	if retained, ok := source.(interface{ Start() int64 }); ok {
		return retained.Start()
	}
	return 0
}

func foldRune(r rune) rune {
	// Use a canonical representative of Unicode's simple-fold cycle (including
	// sigma/Kelvin variants), without changing the original byte offsets.
	minimum := r
	for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
		minimum = min(minimum, next)
	}
	return minimum
}

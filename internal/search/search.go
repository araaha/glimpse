// Package search searches a snapshot of chunked input without assembling lines.
package search

import (
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

// Find performs a literal, rune-aware search. Origin is a byte offset; include
// allows a match exactly at origin. Smart case folds only queries without any
// uppercase letters; ignoreCase folds every query unless smart case overrides it.
// KMP keeps both working memory and read buffers bounded by
// the query size, even on an indefinitely long logical line.
func Find(ctx context.Context, source Source, query string, origin int64, backward, smartCase, ignoreCase, include bool) (Result, error) {
	base := sourceStart(source)
	origin = min(max(base, origin), source.Len())
	var best, wrapped Result
	err := Walk(ctx, source, query, base, source.Len(), smartCase, ignoreCase, func(offset, end int64) bool {
		eligible := offset > origin || (include && offset == origin)
		if backward {
			eligible = offset < origin || (include && offset == origin)
		}
		if eligible {
			best = Result{Offset: offset, Found: true}
			if !backward {
				return false
			}
		} else if backward || !wrapped.Found {
			wrapped = Result{Offset: offset, Found: true, Wrapped: true}
		}
		return true
	})
	if err != nil {
		return Result{}, err
	}
	if best.Found {
		return best, nil
	}
	return wrapped, nil
}

// Walk streams every (including overlapping) literal match in an absolute byte
// range. Returning false from visit stops immediately without retaining matches.
func Walk(ctx context.Context, source Source, query string, start, end int64, smartCase, ignoreCase bool, visit func(start, end int64) bool) error {
	pattern := []rune(query)
	if len(pattern) == 0 {
		return nil
	}
	fold := smartCase || ignoreCase
	for _, r := range pattern {
		if smartCase && unicode.IsUpper(r) {
			fold = false
		}
	}
	if fold {
		for i, r := range pattern {
			pattern[i] = foldRune(r)
		}
	}
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
	end = min(end, source.Len())
	start = min(max(sourceStart(source), start), end)
	offsets := make([]int64, len(pattern))
	reader := searchReader{reader: content.NewReader(source, start, end, 0, true), ctx: ctx}
	index := int64(0)
	matched := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := reader.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if token.Hidden {
			continue
		}
		r := token.Raw
		offsets[index%int64(len(pattern))] = token.Start
		index++
		if fold {
			r = foldRune(r)
		}
		for matched > 0 && pattern[matched] != r {
			matched = prefix[matched-1]
		}
		if pattern[matched] == r {
			matched++
		}
		if matched != len(pattern) {
			continue
		}
		offset := offsets[(index-int64(len(pattern)))%int64(len(pattern))]
		matched = prefix[matched-1] // Preserve overlapping matches.
		if !visit(offset, token.End) {
			return nil
		}
	}
	return nil
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

// Package search searches a snapshot of chunked input without assembling lines.
package search

import (
	"bufio"
	"context"
	"errors"
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
	pattern := []rune(query)
	if len(pattern) == 0 {
		return Result{}, nil
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
	length := source.Len()
	origin = min(max(0, origin), length)
	offsets := make([]int64, len(pattern))
	reader := bufio.NewReaderSize(io.NewSectionReader(source, 0, length), 64*1024)
	var position, index int64
	matched := 0
	var best, wrapped Result
	for {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		r, size, err := reader.ReadRune()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Result{}, err
		}
		offsets[index%int64(len(pattern))] = position
		position += int64(size)
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
		eligible := offset > origin || (include && offset == origin)
		if backward {
			eligible = offset < origin || (include && offset == origin)
		}
		if eligible {
			best = Result{Offset: offset, Found: true}
			if !backward {
				return best, nil
			}
		} else if backward || !wrapped.Found {
			wrapped = Result{Offset: offset, Found: true, Wrapped: true}
		}
	}
	if best.Found {
		return best, nil
	}
	return wrapped, nil
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

package search

import (
	"context"
	"errors"
	"glimpse/internal/content"
	"io"
)

// searchReader strips supported styling and normalizes the adjacent overstrikes
// used by man pages. It holds at most two tokens, never an entire logical line.
type searchReader struct {
	reader                *content.Reader
	ctx                   context.Context
	pending               content.Token
	queued                content.Token
	hasPending, hasQueued bool
	batch                 content.ASCIIRun
	single                [1]byte
	// boundary, when positive, is a proposed restart offset. straddled records
	// a decoded token spanning it, so restarting there would decode differently.
	boundary  int64
	straddled bool
	tokens    int // decoded since the last cancellation check
}

func (r *searchReader) check(start, end int64) {
	if start < r.boundary && end > r.boundary {
		r.straddled = true
	}
}

// ascii emits text proven not to be part of an overstrike, without manufacturing
// one Token (and multiple lookahead/context checks) for each ordinary byte.
func (r *searchReader) ascii() (content.ASCIIRun, bool) {
	if len(r.batch.Bytes) > 0 {
		run := r.batch
		r.batch = content.ASCIIRun{}
		return run, true
	}
	if r.hasQueued || r.hasPending && (r.pending.Raw < ' ' || r.pending.Raw > '~' || r.pending.End != r.pending.Start+1) {
		return content.ASCIIRun{}, false
	}
	if !r.hasPending {
		// Nothing is pending that a backspace past this styling could
		// overstrike, so it can be skipped instead of decoded token by token.
		for {
			token, ok := r.reader.SkipSGR()
			if !ok {
				break
			}
			r.check(token.Start, token.End)
		}
	}
	run, ok := r.reader.ASCII()
	if !ok {
		return run, false
	}
	if r.hasPending {
		r.batch = run
		r.single[0] = byte(r.pending.Raw)
		run = content.ASCIIRun{Bytes: r.single[:], Start: r.pending.Start}
		r.hasPending = false
	}
	return run, true
}

func (r *searchReader) raw() (content.Token, error) {
	if r.hasQueued {
		token := r.queued
		r.hasQueued = false
		return token, nil
	}
	for {
		if r.tokens++; r.tokens%64 == 0 {
			if err := r.ctx.Err(); err != nil {
				return content.Token{}, err
			}
		}
		token, err := r.reader.Next()
		if err != nil {
			return token, err
		}
		r.check(token.Start, token.End)
		if !token.Hidden {
			return token, nil
		}
	}
}

func printable(token content.Token) bool { return token.Raw >= ' ' && token.Raw != 0x7f }

func (r *searchReader) next() (content.Token, error) {
	if !r.hasPending {
		token, err := r.raw()
		if err != nil {
			return token, err
		}
		r.pending, r.hasPending = token, true
	}
	for {
		next, err := r.raw()
		if errors.Is(err, io.EOF) {
			token := r.pending
			r.hasPending = false
			return token, nil
		}
		if err != nil {
			return content.Token{}, err
		}
		if next.Raw == '\b' && printable(r.pending) {
			replacement, err := r.raw()
			if err == nil && printable(replacement) {
				if replacement.Raw == '_' {
					r.pending.End = replacement.End
				} else if replacement.Raw == r.pending.Raw || r.pending.Raw == '_' {
					r.pending.Raw, r.pending.End = replacement.Raw, replacement.End
				} else {
					r.pending = replacement
				}
				continue
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return content.Token{}, err
			}
			if err == nil {
				r.queued, r.hasQueued = replacement, true
			}
		}
		token := r.pending
		r.pending = next
		return token, nil
	}
}

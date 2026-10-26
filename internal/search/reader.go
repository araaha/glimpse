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
}

func (r *searchReader) raw() (content.Token, error) {
	if r.hasQueued {
		token := r.queued
		r.hasQueued = false
		return token, nil
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return content.Token{}, err
		}
		token, err := r.reader.Next()
		if err != nil {
			return token, err
		}
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

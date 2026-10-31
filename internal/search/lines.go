package search

import "context"

// lineQuery matches every decoded newline, so lines end where the display
// index ends them, never at a newline hidden inside an escape string.
var lineQuery = Query{Text: "\n", Literal: true}

// LineRun is a run of consecutive lines that hold a match.
type LineRun struct {
	Start, End int64 // the bytes of the lines, including their newlines
	Line       int64 // the first line's zero-based index within the scan
	Lines      int64
}

// LineScan reports the lines of a range that hold a match. Lines counts the
// complete lines, which end in a decoded newline; the bytes from Tail to the
// end of the range belong to a line that may not be complete yet.
type LineScan struct {
	Runs        []LineRun
	Lines       int64
	Tail        int64
	TailMatched bool // Runs ends with the partial line from Tail
}

// MatchingLines scans [start, end) for lines holding a match of query, in
// chunks scanned in parallel. start must be a line start. The partial line
// after the last newline is considered only with partial; keepFirst reports
// the first line whether or not it matches.
func MatchingLines(ctx context.Context, source Source, query Query, start, end int64, partial, keepFirst bool, options Options) (LineScan, error) {
	if _, err := query.compile(); err != nil {
		return LineScan{}, err
	}
	if start >= end {
		return LineScan{Tail: start}, nil
	}
	bounds, err := chunkBounds(source, start, end, options.ChunkSize)
	if err != nil {
		return LineScan{}, err
	}
	type scanned struct {
		LineScan
		start, end int64
		straddled  bool
	}
	scan := func(chunkStart, chunkEnd int64) (scanned, error) {
		last := chunkEnd == end
		boundary := int64(0)
		if !last {
			boundary = chunkEnd
		}
		var ends []int64
		straddled, _, err := walkChecked(ctx, source, lineQuery, chunkStart, end, chunkEnd-1, boundary, func(_, lineEnd int64) bool {
			ends = append(ends, lineEnd)
			return true
		})
		if err != nil {
			return scanned{}, err
		}
		tail := chunkStart
		if len(ends) > 0 {
			tail = ends[len(ends)-1]
		}
		// Chunks end after a raw newline, so an interior chunk has no
		// partial line unless decoding disagrees, which a straddle reports.
		if !last && tail < chunkEnd {
			ends, tail = append(ends, chunkEnd), chunkEnd
		}
		result := scanned{LineScan: LineScan{Lines: int64(len(ends)), Tail: tail}, start: chunkStart, end: chunkEnd, straddled: straddled}
		mark := func(line int64) {
			lineStart, lineEnd := chunkStart, chunkEnd
			if line > 0 {
				lineStart = ends[line-1]
			}
			if line < int64(len(ends)) {
				lineEnd = ends[line]
			} else {
				result.TailMatched = true
			}
			runs := result.Runs
			if n := len(runs); n > 0 && runs[n-1].Line+runs[n-1].Lines == line {
				runs[n-1].End = lineEnd
				runs[n-1].Lines++
			} else if n == 0 || runs[n-1].Line+runs[n-1].Lines < line {
				result.Runs = append(runs, LineRun{Start: lineStart, End: lineEnd, Line: line, Lines: 1})
			}
		}
		includeTail := last && partial && tail < chunkEnd
		if keepFirst && chunkStart == start && (len(ends) > 0 || includeTail) {
			mark(0)
		}
		var line int64
		err = walk(ctx, source, query, chunkStart, end, chunkEnd-1, func(offset, _ int64) bool {
			for line < int64(len(ends)) && ends[line] <= offset {
				line++
			}
			if line == int64(len(ends)) && !includeTail {
				return false
			}
			mark(line)
			return true
		})
		return result, err
	}
	results, err := scanChunks(ctx, bounds, options.Workers, scan)
	if err != nil {
		return LineScan{}, err
	}
	// As in countChunks, a chunk whose last token spans its end is rescanned
	// together with its successor.
	for i := 0; i < len(results)-1; {
		if !results[i].straddled {
			i++
			continue
		}
		merged, err := scan(results[i].start, results[i+1].end)
		if err != nil {
			return LineScan{}, err
		}
		results[i] = merged
		results = append(results[:i+1], results[i+2:]...)
	}
	var total LineScan
	for _, result := range results {
		for _, run := range result.Runs {
			run.Line += total.Lines
			if n := len(total.Runs); n > 0 && total.Runs[n-1].End == run.Start {
				total.Runs[n-1].End = run.End
				total.Runs[n-1].Lines += run.Lines
			} else {
				total.Runs = append(total.Runs, run)
			}
		}
		total.Lines += result.Lines
		total.Tail, total.TailMatched = result.Tail, result.TailMatched
	}
	return total, nil
}

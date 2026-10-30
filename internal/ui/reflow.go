package ui

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"time"

	"glimpse/internal/content"
)

// reflowSegmentSize is the input laid out by one reflow task.
const reflowSegmentSize = 1 << 20

// A width or policy change replaces every row start, but not where logical
// lines start or the style each one starts in. A reflow therefore splits the
// input already indexed at line starts, lays the segments out on several
// workers, and merges them in order, so the index passes through the same
// states as a sequential one, only sooner.
type reflowSegment struct {
	start, end int64
	style      content.Style
	underline  content.Color
}

type reflowResult struct {
	text *Text // the segment's own layout
	err  error
	done chan struct{}
}

type reflowJob struct {
	segments []reflowSegment
	results  []reflowResult
	next     int           // the next segment to merge
	slots    chan struct{} // bounds segments laid out ahead of the merge
	cancel   context.CancelFunc
	workers  sync.WaitGroup
}

func reflowWorkers() int { return max(1, runtime.GOMAXPROCS(0)-1) }

// planReflow splits the indexed input at line starts, reading each segment's
// starting style from the index about to be replaced. It returns nil when a
// sequential reindex is as good: little input, one worker, or a moved base or
// tracked style history, which this plan does not carry over.
func (t *Text) planReflow(base int64) []reflowSegment {
	if t.starts == nil || t.base != base || t.trackStyles || reflowWorkers() < 2 {
		return nil
	}
	lines := t.lineStarts.count
	if t.open != nil {
		lines = t.open.counts[1] // Later lines may still be rewound.
	}
	if lines < 2 {
		return nil
	}
	end := t.lineStarts.at(lines - 1)
	if end-base < 2*reflowSegmentSize {
		return nil
	}
	var plan []reflowSegment
	for start := base; start < end; {
		next := end
		if target := start + reflowSegmentSize; target < end {
			next = t.lineStarts.at(sort.Search(lines, func(i int) bool { return t.lineStarts.at(i) >= target }))
		}
		style, underline := t.styleOfRow(t.RowAt(start))
		plan = append(plan, reflowSegment{start, next, style, underline})
		start = next
	}
	for _, index := range t.indexes() {
		if index.err != nil {
			return nil
		}
	}
	return plan
}

func (t *Text) startReflow(plan []reflowSegment, width int) {
	ctx, cancel := context.WithCancel(context.Background())
	workers := min(reflowWorkers(), len(plan))
	job := &reflowJob{segments: plan, results: make([]reflowResult, len(plan)), slots: make(chan struct{}, 2*workers), cancel: cancel}
	for i := range job.results {
		job.results[i].done = make(chan struct{})
	}
	var mu sync.Mutex
	next := 0
	for range workers {
		job.workers.Add(1)
		go func() {
			defer job.workers.Done()
			for {
				select {
				case job.slots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(plan) {
					<-job.slots
					return
				}
				result := &job.results[i]
				result.text, result.err = t.layoutSegment(ctx, plan[i], width)
				close(result.done)
			}
		}()
	}
	t.reflow = job
}

// layoutSegment indexes one segment as UpdateFor would, on its own Text.
func (t *Text) layoutSegment(ctx context.Context, segment reflowSegment, width int) (*Text, error) {
	text := &Text{source: t.source, indexMemory: 1 << 40, indexQuota: 1 << 40, nowrap: t.nowrap, wideAmbiguous: t.wideAmbiguous}
	if err := text.reset(width, segment.start, segment.style, segment.underline); err != nil {
		return nil, err
	}
	reader := content.NewGraphemeReader(t.source, segment.start, segment.end, segment.style, true, segment.underline)
	if err := text.scan(ctx, reader, true, time.Time{}); err != nil {
		text.Close()
		return nil, err
	}
	return text, nil
}

// mergeReflow appends laid-out segments in order until the deadline, if any.
// done reports that the reflow is over; sequential indexing continues from
// the merged prefix, which is also the fallback if a segment disagrees.
func (t *Text) mergeReflow(deadline time.Time) (done bool, err error) {
	job := t.reflow
	for job.next < len(job.segments) {
		result := &job.results[job.next]
		if deadline.IsZero() {
			<-result.done
		} else {
			timer := time.NewTimer(time.Until(deadline))
			select {
			case <-result.done:
				timer.Stop()
			case <-timer.C:
				return false, nil
			}
		}
		segment, segmentText := job.segments[job.next], result.text
		consistent := result.err == nil && segmentText.scanned == segment.end && segmentText.column == 0
		if consistent && job.next+1 < len(job.segments) {
			following := job.segments[job.next+1]
			consistent = segmentText.style == following.style && segmentText.underlineColor == following.underline
		}
		if !consistent {
			t.stopReflow()
			return true, nil
		}
		if err := t.appendSegment(segmentText); err != nil {
			return false, err
		}
		t.scanned, t.style, t.underlineColor = segment.end, segmentText.style, segmentText.underlineColor
		t.column, t.struckWidth = 0, 0
		if err := segmentText.Close(); err != nil {
			return false, err
		}
		<-job.slots
		job.next++
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			break
		}
	}
	if job.next < len(job.segments) {
		return false, nil
	}
	t.stopReflow()
	return true, nil
}

// appendSegment adds a segment's rows and lines after its first, which the
// previous segment's final newline (or reset) already added.
func (t *Text) appendSegment(segment *Text) error {
	styleIndex := 0
	for row := 1; row < segment.starts.count; row++ {
		for styleIndex+1 < segment.rowStyleRows.count && segment.rowStyleRows.at(styleIndex+1) <= int64(row) {
			styleIndex++
		}
		t.style, t.underlineColor = content.Style(segment.rowStyles.at(styleIndex)), content.Color(segment.rowUnderlines.at(styleIndex))
		if err := t.addRow(segment.starts.at(row)); err != nil {
			return err
		}
	}
	for line := 1; line < segment.lineStarts.count; line++ {
		if err := t.lineStarts.append(segment.lineStarts.at(line)); err != nil {
			return err
		}
	}
	return nil
}

// stopReflow cancels the workers, waits for them, and drops their results.
func (t *Text) stopReflow() {
	job := t.reflow
	if job == nil {
		return
	}
	job.cancel()
	job.workers.Wait()
	for i := job.next; i < len(job.results); i++ {
		select {
		case <-job.results[i].done:
			if text := job.results[i].text; text != nil {
				text.Close()
			}
		default:
		}
	}
	t.reflow = nil
}

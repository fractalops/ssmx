package workflow

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// stepSpinner animates a braille spinner on the current terminal line.
// The caller prints the initial "  ⠋  name  running..." WITHOUT a trailing
// newline, then calls newStepSpinner. The goroutine overwrites the line via
// \r, accelerating from 150ms → 100ms → 60ms intervals as time passes.
// Call Stop() then ClearLine() before printing the final status line.
type stepSpinner struct {
	w     io.Writer
	name  string
	isTTY bool
	done  chan struct{}
	once  sync.Once
	wg    sync.WaitGroup
}

func newStepSpinner(w io.Writer, name string, isTTY bool) *stepSpinner {
	s := &stepSpinner{
		w:     w,
		name:  name,
		isTTY: isTTY,
		done:  make(chan struct{}),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *stepSpinner) run() {
	defer s.wg.Done()
	start := time.Now()
	i := 1 // frame 0 was already printed by the caller
	for {
		// Accelerate: 150ms for first 2s, 100ms for next 3s, then 60ms.
		elapsed := time.Since(start)
		var interval time.Duration
		switch {
		case elapsed < 2*time.Second:
			interval = 150 * time.Millisecond
		case elapsed < 5*time.Second:
			interval = 100 * time.Millisecond
		default:
			interval = 60 * time.Millisecond
		}
		select {
		case <-s.done:
			return
		case <-time.After(interval):
			frame := ansi(s.isTTY, ansiDim, spinnerFrames[i%len(spinnerFrames)])
			suffix := ansi(s.isTTY, ansiDim, "running...")
			fmt.Fprintf(s.w, "\r  %s  %s  %s", frame, s.name, suffix)
			i++
		}
	}
}

// Stop halts the animation and waits for the goroutine to exit.
// Safe to call multiple times.
func (s *stepSpinner) Stop() {
	s.once.Do(func() { close(s.done) })
	s.wg.Wait()
}

// ClearLine overwrites the spinner line with spaces so the caller can
// print the final status cleanly. Must be called after Stop().
func (s *stepSpinner) ClearLine() {
	width := 5 + len(s.name) + 13 // "  x  " + name + "  running..."
	if width < 60 {
		width = 60
	}
	fmt.Fprintf(s.w, "\r%s\r", strings.Repeat(" ", width))
}

// lockedWriter serialises concurrent writes from multiple goroutines sharing
// a terminal so that lines from different steps in the same DAG level do not
// interleave at the byte level.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (lw *lockedWriter) Write(b []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(b)
}

// progressWriter indents step stdout and stops the spinner on first write.
// Subsequent writes go directly to the underlying writer without the stop
// overhead (the spinner is already gone by then).
type progressWriter struct {
	w      io.Writer
	once   sync.Once
	stopFn func() // called exactly once before the first byte is written
}

func newProgressWriter(w io.Writer, stopFn func()) *progressWriter {
	return &progressWriter{w: w, stopFn: stopFn}
}

func (pw *progressWriter) Write(b []byte) (int, error) {
	pw.once.Do(pw.stopFn)
	// Print each non-empty line with a 4-space indent.
	content := strings.TrimRight(string(b), "\n")
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			fmt.Fprintf(pw.w, "    %s\n", line)
		}
	}
	return len(b), nil
}

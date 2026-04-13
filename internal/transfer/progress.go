package transfer

import (
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"golang.org/x/term"
)

const progressWidth = 30

// progress tracks bytes transferred and renders an inline progress bar to stderr.
// Output is suppressed when stderr is not a TTY (e.g. redirected to a file).
type progressTracker struct {
	name  string
	total int64 // 0 = unknown
	bytes int64
	bar   progress.Model
	start time.Time
	last  time.Time
	isTTY bool
}

func newProgressTracker(name string, total int64) *progressTracker {
	bar := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(progressWidth),
		progress.WithoutPercentage(),
	)
	return &progressTracker{
		name:  name,
		total: total,
		bar:   bar,
		start: time.Now(),
		isTTY: term.IsTerminal(int(os.Stderr.Fd())), //nolint:gosec // uintptr→int conversion safe for fd values
	}
}

func (p *progressTracker) tick(n int) {
	p.bytes += int64(n)
	if p.isTTY && time.Since(p.last) >= 100*time.Millisecond {
		p.render()
		p.last = time.Now()
	}
}

func (p *progressTracker) render() {
	elapsed := time.Since(p.start)
	var speed int64
	if secs := elapsed.Seconds(); secs > 0 {
		speed = int64(float64(p.bytes) / secs)
	}
	name := truncateName(p.name, 24)
	if p.total > 0 {
		pct := float64(p.bytes) / float64(p.total)
		fmt.Fprintf(os.Stderr, "\r  %-24s  %s  %s / %s  %s/s   ",
			name, p.bar.ViewAs(pct),
			formatBytes(p.bytes), formatBytes(p.total),
			formatBytes(speed))
	} else {
		// Unknown total — oscillate the bar so it shows activity.
		secs := time.Since(p.start).Seconds()
		pulse := 0.5 + 0.45*math.Sin(secs*math.Pi*0.8)
		fmt.Fprintf(os.Stderr, "\r  %-24s  %s  %s  %s/s   ",
			name, p.bar.ViewAs(pulse),
			formatBytes(p.bytes), formatBytes(speed))
	}
}

// Done prints the final summary and moves to a new line.
func (p *progressTracker) Done() {
	if !p.isTTY {
		return
	}
	elapsed := time.Since(p.start)
	var avgSpeed int64
	if secs := elapsed.Seconds(); secs > 0 {
		avgSpeed = int64(float64(p.bytes) / secs)
	}
	// Full bar on completion.
	bar := p.bar.ViewAs(1.0)
	if p.total == 0 {
		bar = ""
	}
	fmt.Fprintf(os.Stderr, "\r  %-24s  %s  %s  done  (%s, avg %s/s)\n",
		truncateName(p.name, 24), bar,
		formatBytes(p.bytes), elapsed.Round(time.Millisecond), formatBytes(avgSpeed))
}

// progressReader wraps an io.Reader, tracking bytes read.
type progressReader struct {
	io.Reader
	pt *progressTracker
}

func newProgressReader(r io.Reader, name string, total int64) *progressReader {
	return &progressReader{Reader: r, pt: newProgressTracker(name, total)}
}

func (pr *progressReader) Read(b []byte) (int, error) {
	n, err := pr.Reader.Read(b)
	pr.pt.tick(n)
	return n, err //nolint:wrapcheck // io.Reader interface forwarder
}

func (pr *progressReader) Done() { pr.pt.Done() }

// progressWriter wraps an io.Writer, tracking bytes written.
// Used for CopyRemoteToRemote where total size is unknown.
type progressWriter struct {
	io.Writer
	pt *progressTracker
}

func newProgressWriter(w io.Writer, name string) *progressWriter {
	return &progressWriter{Writer: w, pt: newProgressTracker(name, 0)}
}

func (pw *progressWriter) Write(b []byte) (int, error) {
	n, err := pw.Writer.Write(b)
	pw.pt.tick(n)
	return n, err //nolint:wrapcheck // io.Writer interface forwarder
}

func (pw *progressWriter) Done() { pw.pt.Done() }

func formatBytes(n int64) string {
	switch {
	case n >= 1 << 30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1 << 20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1 << 10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func truncateName(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-(n-3):]
}

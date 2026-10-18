package cli

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestDashboardURLReadsTheBanner(t *testing.T) {
	banner := "Orch dashboard running on http://127.0.0.1:43123\n  localhost: http://127.0.0.1:43123\n"
	if got := dashboardURL(strings.NewReader(banner), time.Second); got != "http://127.0.0.1:43123" {
		t.Errorf("got %q", got)
	}
	if got := dashboardURL(strings.NewReader("listening\n"), time.Second); got != "" {
		t.Errorf("no banner: got %q, want empty", got)
	}
	r, w := io.Pipe()
	defer func() { _ = w.Close() }()
	if got := dashboardURL(r, 50*time.Millisecond); got != "" {
		t.Errorf("silent dashboard: got %q, want empty", got)
	}
}

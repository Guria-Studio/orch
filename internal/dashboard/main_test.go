package dashboard

import (
	"flag"
	"io"
	"log/slog"
	"os"
	"testing"
)

// TestMain quiets slog.Default, which every test server logs to (none sets
// Options.Logger). The error-path tests inject `database is locked` fakes on
// purpose, and their ERROR lines used to fill the output of any failing run
// and read like real SQLite contention (#611). `go test -v` keeps them.
func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Verbose() {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}
	os.Exit(m.Run())
}

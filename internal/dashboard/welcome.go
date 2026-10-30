package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Guria-Studio/orch/internal/tunnel"
)

// Welcome is what `orch dashboard` serves outside a project: the SPA's
// /welcome page and the four steps behind it — pick a folder, plan, apply,
// run. The steps are internal/cli's (the same ones `orch start` takes); this
// type only puts them on HTTP, behind the gates every operator write has.
//
// There is no token: no project holds one yet. The boundary is the loopback
// listener, which NewWelcome insists on, plus a Host and an Origin check, so
// neither another machine nor a web page in the operator's browser can drive
// it.
type Welcome struct {
	flow  WelcomeFlow
	mux   *http.ServeMux
	http  *http.Server
	addr  string
	ready chan struct{}
	bound string
}

// WelcomeFlow is the welcome page's steps. Each mutating step returns an
// error the page shows as is; State is what the page polls.
type WelcomeFlow interface {
	State() any
	Project(ctx context.Context, path string, gitInit bool) error
	Plan(idea string) error
	Apply(ctx context.Context) error
	Run() error
}

// NewWelcome wires the welcome routes over flow, the SPA at every other path.
func NewWelcome(cfg Config, static http.Handler, flow WelcomeFlow) (*Welcome, error) {
	if !tunnel.IsLoopbackHost(cfg.Host) {
		return nil, fmt.Errorf("the welcome page creates projects and runs agents with no token; "+
			"it listens on %s only, not %s", DefaultHost, cfg.Host)
	}
	w := &Welcome{flow: flow, mux: http.NewServeMux(), addr: cfg.Addr(), ready: make(chan struct{})}
	w.mux.HandleFunc("GET /api/welcome", w.handleState)
	w.mux.HandleFunc("POST /api/welcome/project", w.step(func(r *http.Request, b welcomeBody) error {
		return flow.Project(r.Context(), b.Path, b.GitInit)
	}))
	w.mux.HandleFunc("POST /api/welcome/plan", w.step(func(_ *http.Request, b welcomeBody) error {
		return flow.Plan(b.Idea)
	}))
	w.mux.HandleFunc("POST /api/welcome/apply", w.step(func(r *http.Request, _ welcomeBody) error {
		return flow.Apply(r.Context())
	}))
	w.mux.HandleFunc("POST /api/welcome/run", w.step(func(*http.Request, welcomeBody) error {
		return flow.Run()
	}))
	w.mux.Handle("GET /{$}", http.RedirectHandler("/welcome", http.StatusFound))
	w.mux.Handle("/", static)
	w.http = &http.Server{
		Handler:           w.localOnly(w.mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return w, nil
}

type welcomeBody struct {
	Path    string `json:"path"`
	GitInit bool   `json:"git_init"`
	Idea    string `json:"idea"`
}

// localOnly refuses a request whose Host is not this machine's: a DNS
// rebinding page reaches the listener under its own name.
func (w *Welcome) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if !tunnel.IsLoopbackHost(tunnel.ExtractHost(r.Host)) {
			writePlain(rw, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(rw, r)
	})
}

func (w *Welcome) handleState(rw http.ResponseWriter, _ *http.Request) {
	writeJSON(rw, http.StatusOK, w.flow.State())
}

// step runs one mutating step and answers with the new state, or 400 and the
// step's error as `detail`.
func (w *Welcome) step(run func(*http.Request, welcomeBody) error) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if !sameOriginLocal(r) {
			writePlain(rw, http.StatusForbidden, "forbidden")
			return
		}
		var body welcomeBody
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 64<<10)).Decode(&body); err != nil {
				writeJSON(rw, http.StatusBadRequest, map[string]string{"detail": "invalid JSON body"})
				return
			}
		}
		if err := run(r, body); err != nil {
			writeJSON(rw, http.StatusBadRequest, map[string]string{"detail": err.Error()})
			return
		}
		writeJSON(rw, http.StatusOK, w.flow.State())
	}
}

// Handler exposes the routes, for a test that wants them without a listener.
func (w *Welcome) Handler() http.Handler { return w.http.Handler }

// Ready closes once the listener is open (or has failed to open).
func (w *Welcome) Ready() <-chan struct{} { return w.ready }

// BoundAddr is the address the listener actually got.
func (w *Welcome) BoundAddr() string { return w.bound }

// Serve listens and blocks until ctx is cancelled.
func (w *Welcome) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", w.addr)
	if err != nil {
		close(w.ready)
		return fmt.Errorf("listen on %s: %w", w.addr, err)
	}
	w.bound = ln.Addr().String()
	close(w.ready)
	errCh := make(chan error, 1)
	go func() {
		if err := w.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.http.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("welcome shutdown: %w", err)
	}
	return <-errCh
}

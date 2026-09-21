// Package server exposes the dashboard and its JSON API.
//
// Every read is served from the in-memory snapshot, so handlers return in
// microseconds regardless of how slow the underlying GitHub sweep was.
package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Tzomily-Anvar/argus/internal/config"
	"github.com/Tzomily-Anvar/argus/internal/rules"
	"github.com/Tzomily-Anvar/argus/internal/sprint"
	"github.com/Tzomily-Anvar/argus/internal/store"
	"github.com/Tzomily-Anvar/argus/internal/sweep"
	"github.com/Tzomily-Anvar/argus/web"
)

type Server struct {
	cache *sweep.Cache
	mux   *http.ServeMux

	// Set only when the sprint tool is configured. The frontend asks
	// /api/tools which tools are live, so an unconfigured one is absent
	// rather than present and failing.
	sprint *sprint.Service
	store  store.Store
}

func New(cache *sweep.Cache) *Server {
	s := &Server{cache: cache, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	// The healthcheck also says which Argus this is. Someone running the
	// container and the binary at once otherwise gets a cheerful "running"
	// from `argus doctor` that is really the other one answering.
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "ok",
			"container": config.InContainer(),
			"pid":       os.Getpid(),
		})
	})

	// The whole dashboard in one call: identity, freshness, and every
	// rule's latest rows.
	s.mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.cache.Snapshot())
	})

	// The rule catalogue: what exists, whether it is on, and the exact
	// environment variable for each knob. This is generated from the
	// registry, so a rule added tomorrow documents itself here today.
	s.mux.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
		all := rules.All()
		out := make([]map[string]any, 0, len(all))
		for _, rule := range all {
			out = append(out, rules.Describe(rule))
		}
		writeJSON(w, http.StatusOK, map[string]any{"rules": out})
	})

	s.mux.HandleFunc("GET /api/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		rule, ok := rules.Get(r.PathValue("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such rule"})
			return
		}
		writeJSON(w, http.StatusOK, rules.Describe(rule))
	})

	// Asks the background sweeper to run now. Returns immediately - the
	// client keeps polling the snapshot and sees `sweeping` go false.
	s.mux.HandleFunc("POST /api/refresh", func(w http.ResponseWriter, r *http.Request) {
		s.cache.Refresh()
		writeJSON(w, http.StatusAccepted, map[string]bool{"queued": true})
	})

	// Which tools are live. The rail renders from this, so a tool without
	// credentials never appears rather than appearing and failing when
	// clicked - the same check the original Argus made of its backends.
	s.mux.HandleFunc("GET /api/tools", func(w http.ResponseWriter, r *http.Request) {
		tools := []map[string]any{
			{"id": "pr", "label": "Pull requests", "available": config.ToolEnabled("pr")},
			{"id": "sprint", "label": "Sprint reports", "available": s.sprint != nil},
		}
		writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
	})

	s.mux.Handle("GET /", s.static())
}

// static serves the built dashboard, falling back to index.html so the
// client-side router owns unknown paths.
func (s *Server) static() http.Handler {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Printf("server: embedded assets unavailable: %v", err)
		return http.HandlerFunc(notBuilt)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return http.HandlerFunc(notBuilt)
	}
	files := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(dist, clean); err != nil {
			// Unknown path: hand it to the SPA rather than 404.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		// Hashed asset filenames are immutable; index.html must not be
		// cached or a rebuild would keep serving the old bundle.
		if strings.HasPrefix(clean, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func notBuilt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!doctype html><html><body style="font-family:system-ui;max-width:40rem;margin:4rem auto">
<h1>Dashboard not built</h1>
<p>The API is running, but the frontend bundle is missing. Build it with:</p>
<pre>cd web &amp;&amp; npm install &amp;&amp; npm run build</pre>
<p>The Docker image does this for you; this message means you are running a bare <code>go build</code>.</p>
<p>The JSON API works regardless: <a href="/api/snapshot">/api/snapshot</a></p>
</body></html>`))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("server: encoding response: %v", err)
	}
}

// Run starts the HTTP server with timeouts suited to a local dashboard.
func Run(addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}

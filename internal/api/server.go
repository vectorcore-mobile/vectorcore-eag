package api

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"gorm.io/gorm"

	"github.com/vectorcore/eag/internal/config"
	"github.com/vectorcore/eag/internal/expiry"
	"github.com/vectorcore/eag/internal/feeds"
	"github.com/vectorcore/eag/internal/xmpp"
)

type Server struct {
	router  *chi.Mux
	db      *gorm.DB
	manager *feeds.Manager
	xmpp    *xmpp.Server
	expiry  *expiry.Worker
	xmppCfg *config.XMPPServerConfig
	startAt int64
	version string
}

func NewServer(
	db *gorm.DB,
	manager *feeds.Manager,
	xmppSrv *xmpp.Server,
	exp *expiry.Worker,
	xmppCfg *config.XMPPServerConfig,
	startAt int64,
	version string,
	webFS embed.FS,
	logWriter io.Writer,
) *Server {
	s := &Server{
		router:  chi.NewMux(),
		db:      db,
		manager: manager,
		xmpp:    xmppSrv,
		expiry:  exp,
		xmppCfg: xmppCfg,
		startAt: startAt,
		version: version,
	}
	s.setup(webFS, logWriter)
	return s
}

func (s *Server) setup(webFS embed.FS, logWriter io.Writer) {
	r := s.router
	r.Use(middleware.RequestLogger(&middleware.DefaultLogFormatter{
		Logger:  log.New(logWriter, "", log.LstdFlags),
		NoColor: true,
	}))
	r.Use(middleware.Recoverer)
	r.Use(cors.AllowAll().Handler) // TODO: restrict origins in production

	api := humachi.New(r, huma.DefaultConfig("VectorCore EAG API", "1.0.0"))

	registerAlertHandlers(api, s.db)
	registerFeedHandlers(api, s.db, s.manager)
	registerXMPPHandlers(api, s.db, s.xmpp)
	registerSystemHandlers(api, s.db, s.xmpp, s.expiry, s.xmppCfg, s.startAt, s.version)
	registerCBEHandlers(api, s.db, s.manager)
	registerGeoCodeHandlers(api, s.db)

	// SSE endpoint — pushes a "peer-change" event when a peer connects or disconnects.
	r.Get("/api/v1/system/events", s.handleSSE)

	// SPA fallback: serve web/dist for all non-API routes
	distFS, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		panic(fmt.Sprintf("web/dist embed: %v", err))
	}
	fileServer := http.FileServer(http.FS(distFS))
	r.Handle("/*", spaHandler(distFS, fileServer))
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // prevent nginx from buffering the stream

	ch := s.xmpp.Subscribe()
	defer s.xmpp.Unsubscribe(ch)

	// Initial comment so the browser knows the connection is live.
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprintf(w, "event: peer-change\ndata: {}\n\n")
			flusher.Flush()
		case <-ticker.C:
			// Keepalive comment to prevent proxy/browser timeouts.
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) Start(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint:errcheck
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// spaHandler serves static files from the embed FS and falls back to serving
// index.html directly for any unknown path (supports client-side routing).
// We serve index.html via http.ServeContent rather than delegating back to
// the file server, because http.FileServer redirects "/index.html" → "/" which
// would create an infinite redirect loop.
func spaHandler(distFS fs.FS, fileServer http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash for FS lookup
		path := r.URL.Path
		if len(path) > 0 && path[0] == '/' {
			path = path[1:]
		}

		// If the exact file exists in the embed FS, serve it normally
		if path != "" {
			if _, err := fs.Stat(distFS, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Fall back: serve index.html content directly (no redirect)
		f, err := distFS.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "index.html", stat.ModTime(), f.(io.ReadSeeker))
	}
}

package web

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/je4/utils/v2/pkg/zLogger"
	"github.com/je4/wsbrokerdisplayclient/data"
	"github.com/je4/wsbrokerdisplayclient/pkg/manager"
)

// Server represents the web dashboard and REST/HTMX server.
type Server struct {
	router     *gin.Engine
	httpServer *http.Server
	manager    *manager.Manager
	listenAddr string
	extAddr    string
	clientName string
	brokerURL  string
	logger     zLogger.ZLogger
	mu         sync.Mutex
}

// Config defines options for creating the Server.
type Config struct {
	ListenAddr string
	ExtAddr    string
	ClientName string
	BrokerURL  string
}

// NewServer initializes and configures the Gin web server.
func NewServer(cfg Config, mgr *manager.Manager, logger zLogger.ZLogger) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "localhost:8770"
	}
	if cfg.ExtAddr == "" {
		cfg.ExtAddr = fmt.Sprintf("http://%s", cfg.ListenAddr)
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	// Configure CORS
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "HX-Request", "HX-Trigger", "HX-Target", "HX-Current-URL"}
	router.Use(cors.New(corsConfig))

	// Parse embedded HTML templates
	tmpl, err := template.ParseFS(data.WebFS, "web/templates/*.gohtml")
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse embedded templates")
	}
	router.SetHTMLTemplate(tmpl)

	// Serve embedded static assets
	staticFS, err := fs.Sub(data.WebFS, "web/static")
	if err != nil {
		return nil, errors.Wrap(err, "failed to load embedded static filesystem")
	}
	router.StaticFS("/static", http.FS(staticFS))

	s := &Server{
		router:     router,
		manager:    mgr,
		listenAddr: cfg.ListenAddr,
		extAddr:    cfg.ExtAddr,
		clientName: cfg.ClientName,
		brokerURL:  cfg.BrokerURL,
		logger:     logger,
	}

	s.setupRoutes()
	return s, nil
}

// Router returns the underlying gin.Engine (useful for testing).
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Start launches the HTTP server in the background.
func (s *Server) Start(wg *sync.WaitGroup) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.httpServer = &http.Server{
		Addr:    s.listenAddr,
		Handler: s.router,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if s.logger != nil {
			s.logger.Info().Str("addr", s.listenAddr).Msg("starting web dashboard controller server")
		}
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if s.logger != nil {
				s.logger.Error().Err(err).Msg("web dashboard controller server terminated with error")
			}
		}
	}()
}

// GracefulStop cleanly shuts down the HTTP server within a 5-second timeout.
func (s *Server) GracefulStop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			if s.logger != nil {
				s.logger.Warn().Err(err).Msg("error during web server graceful shutdown")
			}
		}
	}
}

// GetListenerAddr returns the actual listening address (useful for dynamic port allocations).
func (s *Server) GetListenerAddr() net.Addr {
	return nil
}

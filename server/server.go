package server

import (
	"context"
	"embed"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/Polypheides/go-homelab-cable/logger"
	"github.com/Polypheides/go-homelab-cable/network"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var Log = logger.For("server")
var ErrorLog = logger.For("error")

var (
	//go:embed static/*
	staticFS embed.FS
	//go:embed templates/*.html
	templatesFS embed.FS
)

type Server struct {
	port    string
	Network *network.Network
	echo    *echo.Echo
}

// --- Core Lifecycle ---

// NewServer initializes a new web server instance for the given network.
func NewServer(port string, n *network.Network) *Server {
	return &Server{
		port:    port,
		Network: n,
	}
}

// Serve starts the web server and defines all API, HTMX, and streaming routes.
func (s *Server) Serve(ready chan<- struct{}) {
	s.echo = echo.New()
	s.echo.HideBanner = true

	if ready != nil {
		close(ready)
	}

	s.echo.Use(middleware.Recover())

	s.echo.StaticFS("/", echo.MustSubFS(staticFS, "static"))

	renderer := &TemplateRenderer{
		templates: template.Must(template.ParseFS(templatesFS, "templates/*.html")),
	}

	s.echo.Renderer = renderer
	s.echo.GET("/api/networks", s.getNetworks)

	s.echo.GET("/api/networks/:callsign/channels", s.getChannels)
	s.echo.GET("/api/networks/:callsign/channels/:channel_id", s.getChannel)
	s.echo.PUT("/api/networks/:callsign/channels/:channel_id/set_live", s.setChannelLive)
	s.echo.PUT("/api/networks/:callsign/channels/:channel_id/play_next", s.playNext)
	s.echo.PUT("/api/networks/:callsign/channels/:channel_id/play_previous", s.playPrevious)
	s.echo.GET("/api/networks/:callsign/live", s.liveChannel)

	s.echo.PUT("/api/networks/:callsign/live/next", s.playLiveNext)
	s.echo.PUT("/api/networks/:callsign/live/previous", s.playLivePrevious)
	s.echo.PUT("/api/networks/:callsign/live/repair", s.repairLive)
	s.echo.PUT("/api/networks/:callsign/live/recall", s.recallLive)
	s.echo.PUT("/api/networks/:callsign/repair_all", s.repairAll)

	s.echo.POST("/api/networks/:callsign/channels", s.addChannel)
	s.echo.GET("/api/status/binaries", s.getBinaryStatus)

	s.echo.GET("/htmx/status", s.getHtmxStatus)
	s.echo.GET("/htmx/remote_status", s.getHtmxRemoteStatus)
	s.echo.PUT("/htmx/channels/:channel_id/next", s.playNext)
	s.echo.PUT("/htmx/channels/:channel_id/previous", s.playPrevious)
	s.echo.PUT("/htmx/channels/:channel_id/tune", s.htmxTune)
	s.echo.PUT("/htmx/channels/live/next", s.playLiveNext)
	s.echo.PUT("/htmx/channels/live/previous", s.playLivePrevious)
	s.echo.PUT("/htmx/channels/live/repair", s.repairLive)
	s.echo.PUT("/htmx/channels/repair_all", s.repairAll)
	s.echo.PUT("/htmx/channels/live/recall", s.recallLive)

	s.echo.GET("/master", s.streamMaster)
	s.echo.GET("/:channel_num/", s.streamChannel)

	// Hide the default Echo startup log and use our standardized format
	Log.Printf("[Server] Web interface listening on port %s\n", s.port)
	if err := s.echo.Start(fmt.Sprintf(":%s", s.port)); err != nil && err != http.ErrServerClosed {
		s.echo.Logger.Error(err)
	}
}

// Stop gracefully shuts down the web server.
func (s *Server) Stop() error {
	if s.echo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.echo.Shutdown(ctx)
	}
	return nil
}

// --- Streaming Handlers ---

type Streamer interface {
	Stream(context.Context, io.Writer) error
}

// handleStream is a common helper that writes MPEG-TS data from a hub or relay to an HTTP response.
func (s *Server) handleStream(c echo.Context, st Streamer) error {
	c.Response().Header().Set(echo.HeaderContentType, "video/mp2t")
	c.Response().WriteHeader(http.StatusOK)
	return st.Stream(c.Request().Context(), c.Response().Writer)
}

// streamMaster handles the HTTP streaming request for the master relay.
func (s *Server) streamMaster(c echo.Context) error {
	return s.handleStream(c, s.Network.MasterBroadcaster())
}

// streamChannel handles the HTTP streaming request for a specific channel.
func (s *Server) streamChannel(c echo.Context) error {
	num, err := strconv.Atoi(c.Param("channel_num"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid channel number")
	}

	ch, err := s.Network.ChannelByNumber(num)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "channel not found")
	}

	return s.handleStream(c, ch.Broadcaster().Hub())
}

// --- Internal Helpers ---

// logAction prints a formatted access log message to the console.
func (s *Server) logAction(method, uri string, c *network.Channel) {
	// Clean up the show title/display name
	showTitle := filepath.Base(filepath.Dir(c.Current()))

	logger.For("player").Printf("[%s] [Server] TUNED to CH %d | %s (%s) | %s\n",
		time.Now().Format("15:04:05"),
		c.Number,
		showTitle,
		c.SortMode(),
		filepath.Base(c.Current()),
	)
}

// getHost detects the local IP or host for generating stream URLs.
func (s *Server) getHost(e echo.Context) string {
	host := e.Request().Host
	if host == "" || host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "localhost:") {
		host = network.GetLocalIP() + ":" + s.Network.WebServerPort
	}
	return host
}

package server

import (
	"embed"
	"fmt"
	"text/template"

	"github.com/Polypheides/go-homelab-cable/network"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"path/filepath"
	"strconv"
	"time"
)

var (
	//go:embed static/*
	staticFS embed.FS
	//go:embed templates/*.html
	templatesFS embed.FS
)

type Server struct {
	port    string
	Network *network.Network
}

// NewServer initializes a new web server instance for the given network.
func NewServer(port string, n *network.Network) *Server {
	return &Server{
		port:    port,
		Network: n,
	}
}

// Serve starts the web server and defines all API, HTMX, and streaming routes.
func (s *Server) Serve() {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.Recover())

	e.StaticFS("/", echo.MustSubFS(staticFS, "static"))

	renderer := &TemplateRenderer{
		templates: template.Must(template.ParseFS(templatesFS, "templates/*.html")),
	}

	e.Renderer = renderer
	e.GET("/api/networks", s.getNetworks)

	e.GET("/api/networks/:callsign/channels", s.getChannels)
	e.GET("/api/networks/:callsign/channels/:channel_id", s.getChannel)
	e.PUT("/api/networks/:callsign/channels/:channel_id/set_live", s.setChannelLive)
	e.PUT("/api/networks/:callsign/channels/:channel_id/play_next", s.playNext)
	e.GET("/api/networks/:callsign/live", s.liveChannel)

	e.PUT("/api/networks/:callsign/live/next", s.playLiveNext)

	e.GET("/htmx/status", s.getHtmxStatus)
	e.PUT("/htmx/channels/:channel_id/next", s.htmxPlayNext)
	e.PUT("/htmx/channels/:channel_id/previous", s.htmxPlayPrevious)
	e.PUT("/htmx/channels/:channel_id/tune", s.htmxTune)
	e.PUT("/htmx/live/next", s.htmxPlayLiveNext)

	e.GET("/master", s.streamMaster)
	e.GET("/:channel_num/", s.streamChannel)

	e.Logger.Fatal(e.Start(fmt.Sprintf(":%s", s.port)))
}

// streamMaster handles the HTTP streaming request for the master relay.
func (s *Server) streamMaster(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, "video/mp2t")
	c.Response().WriteHeader(200)

	err := s.Network.MasterBroadcaster().Stream(c.Request().Context(), c.Response().Writer)
	if err != nil {
		return err
	}
	return nil
}

// streamChannel handles the HTTP streaming request for a specific channel.
func (s *Server) streamChannel(c echo.Context) error {
	numStr := c.Param("channel_num")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return echo.NewHTTPError(400, "invalid channel number")
	}

	ch, err := s.Network.ChannelByNumber(num)
	if err != nil {
		return echo.NewHTTPError(404, "channel not found")
	}

	c.Response().Header().Set(echo.HeaderContentType, "video/mp2t")
	c.Response().WriteHeader(200)

	err = ch.Broadcaster().Hub().Stream(c.Request().Context(), c.Response().Writer)
	if err != nil {
		return err
	}
	return nil
}

// logAction prints a formatted access log message to the console.
func (s *Server) logAction(method, uri string, c *network.Channel) {
	contextPart := ""
	if c.Season() > 0 {
		contextPart = fmt.Sprintf(" | S%d:%s", c.Season(), c.SortMode())
	} else {
		contextPart = fmt.Sprintf(" | %s", c.SortMode())
	}

	fmt.Printf("[%s] 200 | %s %s | CH %d%s | %s\n",
		time.Now().Format("15:04:05"),
		method, uri,
		c.Number,
		contextPart,
		filepath.Base(c.Current()),
	)
}

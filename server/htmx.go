package server

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"text/template"

	"github.com/Polypheides/go-homelab-cable/domain"
	"github.com/Polypheides/go-homelab-cable/network"
	"github.com/labstack/echo/v4"
)

type TemplateRenderer struct {
	templates *template.Template
}

// Render executes HTML templates for the web dashboard.
func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

type Meta struct {
	Name  string
	Owner string
}

// getHtmxStatus renders the main dashboard view with the current status of all channels.
func (s *Server) getHtmxStatus(e echo.Context) error {
	channels := s.Network.Channels()
	models := make([]domain.Channel, 0, len(channels))
	host := e.Request().Host
	if host == "" || host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "localhost:") {
		host = network.GetLocalIP() + ":" + s.Network.WebServerPort
	}
	for _, c := range channels {
		models = append(models, domain.ToChannelModel(s.Network, c, host))
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].StreamURL < models[j].StreamURL
	})

	data := struct {
		Name     string
		Owner    string
		Channels []domain.Channel
	}{
		Name:     s.Network.Name,
		Owner:    s.Network.Owner,
		Channels: models,
	}

	return e.Render(http.StatusOK, "status.html", data)
}

// htmxPlayNext advances the specified channel via an HTMX request.
func (s *Server) htmxPlayNext(e echo.Context) error {
	c, err := s.Network.Channel(e.Param("channel_id"))
	if err == nil {
		next := c.PlayNext()
		fmt.Printf("[Next] CH %d -> %s\n", c.Number, next)
		s.logAction("PUT", e.Request().URL.Path, c)
	}
	return e.NoContent(http.StatusNoContent)
}

// htmxPlayLiveNext advances the live channel via an HTMX request.
func (s *Server) htmxPlayLiveNext(e echo.Context) error {
	c, err := s.Network.CurrentChannel()
	if err == nil {
		next := c.PlayNext()
		fmt.Printf("[LiveNext] CH %d -> %s\n", c.Number, next)
		s.logAction("PUT", e.Request().URL.Path, c)
	}
	return e.NoContent(http.StatusNoContent)
}

// htmxPlayPrevious rewinds the specified channel via an HTMX request.
func (s *Server) htmxPlayPrevious(e echo.Context) error {
	c, err := s.Network.Channel(e.Param("channel_id"))
	if err == nil {
		prev := c.PlayPrevious()
		fmt.Printf("[Prev] CH %d -> %s\n", c.Number, prev)
		s.logAction("PUT", e.Request().URL.Path, c)
	}
	return e.NoContent(http.StatusNoContent)
}

// htmxTune sets a new channel as the live tuned channel via an HTMX request.
func (s *Server) htmxTune(e echo.Context) error {
	id := e.Param("channel_id")
	_ = s.Network.SetChannelLive(id)

	c, err := s.Network.Channel(id)
	if err == nil {
		s.logAction("TUNE", e.Request().URL.Path, c)
	}
	return e.NoContent(http.StatusNoContent)
}

package server

import (
	"io"
	"net/http"
	"net/url"
	"sort"
	"text/template"

	"github.com/Polypheides/go-homelab-cable/domain"
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

func (s *Server) buildStatusData(e echo.Context) interface{} {
	channels := s.Network.Channels()
	models := make([]domain.Channel, 0, len(channels))
	host := s.getHost(e)
	for _, c := range channels {
		models = append(models, domain.ToChannelModel(s.Network, c, host))
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].Number < models[j].Number // Sort by channel number for the remote
	})

	return struct {
		Name            string
		Owner           string
		CallSign        string
		CallSignEscaped string
		Channels        []domain.Channel
	}{
		Name:            s.Network.Name,
		Owner:           s.Network.Owner,
		CallSign:        s.Network.CallSign,
		CallSignEscaped: url.PathEscape(s.Network.CallSign),
		Channels:        models,
	}
}

// getHtmxStatus renders the main dashboard view with the current status of all channels.
func (s *Server) getHtmxStatus(e echo.Context) error {
	return e.Render(http.StatusOK, "status.html", s.buildStatusData(e))
}

// getHtmxRemoteStatus renders the mobile remote control view.
func (s *Server) getHtmxRemoteStatus(e echo.Context) error {
	return e.Render(http.StatusOK, "remote_status.html", s.buildStatusData(e))
}

// htmxTune sets a new channel as the live tuned channel via an HTMX request.
func (s *Server) htmxTune(e echo.Context) error {
	id := e.Param("channel_id")
	_ = s.Network.SetChannelLive(id)

	c, err := s.Network.Channel(id)
	if err == nil {
		s.logAction("TUNE", e.Request().URL.Path, c)
	}
	e.Response().Header().Set("HX-Trigger", "refreshStatus")
	return e.NoContent(http.StatusNoContent)
}

package server

import (
	"io"
	"net/http"
	"sort"
	"text/template"

	"github.com/Polypheides/go-homelab-cable/domain"
	"github.com/labstack/echo/v4"
)

type TemplateRenderer struct {
	templates *template.Template
}

func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

type Meta struct {
	Name  string
	Owner string
}

// getHtmxStatus returns the full status of all channels for the web dashboard
func (s *Server) getHtmxStatus(e echo.Context) error {
	channels := s.Network.Channels()
	models := make([]domain.Channel, 0, len(channels))
	for _, c := range channels {
		models = append(models, domain.ToChannelModel(s.Network, c))
	}

	// Sort by StreamURL (Port) to keep the UI stable
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

func (s *Server) htmxPlayNext(e echo.Context) error {
	c, err := s.Network.Channel(e.Param("channel_id"))
	if err == nil {
		_ = c.PlayNext()
		s.logAction("PUT", e.Request().URL.Path, c)
	}
	return e.NoContent(http.StatusNoContent)
}

func (s *Server) htmxPlayLiveNext(e echo.Context) error {
	c, err := s.Network.CurrentChannel()
	if err == nil {
		_ = c.PlayNext()
		s.logAction("PUT", e.Request().URL.Path, c)
	}
	return e.NoContent(http.StatusNoContent)
}

func (s *Server) htmxPlayPrevious(e echo.Context) error {
	c, err := s.Network.Channel(e.Param("channel_id"))
	if err == nil {
		_ = c.PlayPrevious()
		s.logAction("PUT", e.Request().URL.Path, c)
	}
	return e.NoContent(http.StatusNoContent)
}

func (s *Server) htmxTune(e echo.Context) error {
	id := e.Param("channel_id")
	_ = s.Network.SetChannelLive(id)
	
	c, err := s.Network.Channel(id)
	if err == nil {
		s.logAction("TUNE", e.Request().URL.Path, c)
	}
	return e.NoContent(http.StatusNoContent)
}

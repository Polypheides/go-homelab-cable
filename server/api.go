package server

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Polypheides/go-homelab-cable/domain"
	"github.com/Polypheides/go-homelab-cable/network"
	"github.com/labstack/echo/v4"
)

// getNetworks returns a list of all networks managed by the server.
func (s *Server) getNetworks(e echo.Context) error {
	host := e.Request().Host
	if host == "" || host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "localhost:") {
		host = network.GetLocalIP() + ":" + s.Network.WebServerPort
	}
	return e.JSON(http.StatusOK, []domain.Network{
		{
			Name:                s.Network.Name,
			Owner:               s.Network.Owner,
			CallSign:            s.Network.CallSign,
			MasterStreamURL:     s.Network.MasterStreamURL(),
			HttpMasterStreamURL: fmt.Sprintf("http://%s/master", host),
		},
	})
}

// getChannels retrieves a sorted list of all channels on the current network.
func (s *Server) getChannels(e echo.Context) error {
	channels := s.Network.Channels()
	models := make([]domain.Channel, 0, len(channels))
	host := e.Request().Host
	for _, c := range channels {
		models = append(models, domain.ToChannelModel(s.Network, c, host))
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].StreamURL < models[j].StreamURL
	})

	return e.JSON(http.StatusOK, models)
}

// getChannel retrieves metadata for a specific channel by ID.
func (s *Server) getChannel(e echo.Context) error {
	c, err := s.Network.Channel(e.Param("channel_id"))
	if err != nil {
		if errors.Is(err, network.ErrNetworkChannelNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	return s.jsonChannel(e, c)
}

// setChannelLive tunes the network's master relay to the specified channel.
func (s *Server) setChannelLive(e echo.Context) error {
	idParam := e.Param("channel_id")

	var c *network.Channel
	var err error

	if num, parseErr := strconv.Atoi(idParam); parseErr == nil {
		c, err = s.Network.ChannelByNumber(num)
	} else {
		c, err = s.Network.Channel(idParam)
	}

	if err != nil {
		if errors.Is(err, network.ErrNetworkChannelNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}

	err = s.Network.SetChannelLive(c.ID)
	if err != nil {
		return err
	}
	s.logAction("TUNE", e.Request().URL.Path, c)
	return s.jsonChannel(e, c)
}

// playNext advances the specified channel to its next media item.
func (s *Server) playNext(e echo.Context) error {
	c, err := s.Network.Channel(e.Param("channel_id"))
	if err != nil {
		if errors.Is(err, network.ErrNetworkChannelNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	_ = c.PlayNext()
	s.logAction("PUT", e.Request().URL.Path, c)
	return s.jsonChannel(e, c)
}

// playLiveNext advances the currently tuned live channel to its next media item.
func (s *Server) playLiveNext(e echo.Context) error {
	c, err := s.Network.CurrentChannel()
	if err != nil {
		if errors.Is(err, network.ErrNetworkNoChannelPlaying) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	_ = c.PlayNext()
	s.logAction("PUT", e.Request().URL.Path, c)
	return s.jsonChannel(e, c)
}

// liveChannel returns metadata for the currently tuned live channel.
func (s *Server) liveChannel(e echo.Context) error {
	c, err := s.Network.CurrentChannel()
	if err != nil {
		if errors.Is(err, network.ErrNetworkNoChannelPlaying) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	return s.jsonChannel(e, c)
}

// jsonChannel is a helper that renders a channel domain model as a JSON response.
func (s *Server) jsonChannel(e echo.Context, c *network.Channel) error {
	return e.JSON(http.StatusOK, domain.ToChannelModel(s.Network, c, e.Request().Host))
}

package server

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/Polypheides/go-homelab-cable/domain"
	"github.com/Polypheides/go-homelab-cable/network"
	"github.com/Polypheides/go-homelab-cable/player"
	"github.com/labstack/echo/v4"
)

// --- Internal Helpers ---

// setHtmxTrigger is a helper that adds the refreshStatus trigger if the request is from HTMX.
func setHtmxTrigger(e echo.Context) {
	if e.Request().Header.Get("HX-Request") == "true" {
		e.Response().Header().Set("HX-Trigger", "refreshStatus")
	}
}

// jsonChannel is a helper that renders a channel domain model as a JSON response.
func (s *Server) jsonChannel(e echo.Context, c *network.Channel) error {
	return e.JSON(http.StatusOK, domain.ToChannelModel(s.Network, c, s.getHost(e)))
}

// --- General Information ---

// getNetworks returns a list of all networks managed by the server.
func (s *Server) getNetworks(e echo.Context) error {
	host := s.getHost(e)
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

// getBinaryStatus returns the availability of FFmpeg and FFprobe.
func (s *Server) getBinaryStatus(e echo.Context) error {
	ffmpeg, ffprobe := player.CheckStatus()
	return e.JSON(http.StatusOK, map[string]bool{
		"ffmpeg":  ffmpeg,
		"ffprobe": ffprobe,
	})
}

// --- Channel Management ---

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

// addChannel dynamically registers a new media path as a network channel.
func (s *Server) addChannel(e echo.Context) error {
	callsign := e.Param("callsign")
	if s.Network.CallSign != callsign {
		return echo.NewHTTPError(http.StatusNotFound, "network not found")
	}

	var req struct {
		Path   string `json:"path"`
		Season int    `json:"season"`
		Mode   string `json:"mode"`
	}
	if err := e.Bind(&req); err != nil {
		return err
	}

	ch, err := s.Network.AddChannelFromPath(req.Path, req.Season, req.Mode)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return s.jsonChannel(e, ch)
}

// --- Network-Wide Controls ---

// repairAll restarts every channel on the network.
func (s *Server) repairAll(e echo.Context) error {
	_ = s.Network.RepairAll()
	setHtmxTrigger(e)
	return e.NoContent(http.StatusNoContent)
}

// --- Live Playback API ---

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
	setHtmxTrigger(e)
	return s.jsonChannel(e, c)
}

// repairLive restarts the current live stream.
func (s *Server) repairLive(e echo.Context) error {
	_ = s.Network.RepairLive()
	setHtmxTrigger(e)
	return e.NoContent(http.StatusNoContent)
}

// recallLive switches back to the previous channel.
func (s *Server) recallLive(e echo.Context) error {
	_ = s.Network.RecallLive()
	setHtmxTrigger(e)
	return e.NoContent(http.StatusNoContent)
}

// playLiveNext advances the currently tuned live channel to its next media item.
func (s *Server) playLiveNext(e echo.Context) error {
	c, err := s.Network.PlayLiveNext()
	if err != nil {
		if errors.Is(err, network.ErrNetworkNoChannelPlaying) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	s.logAction("PUT", e.Request().URL.Path, c)
	setHtmxTrigger(e)
	return s.jsonChannel(e, c)
}

// playLivePrevious rewinds the currently tuned live channel to its previous media item.
func (s *Server) playLivePrevious(e echo.Context) error {
	c, err := s.Network.PlayLivePrevious()
	if err != nil {
		if errors.Is(err, network.ErrNetworkNoChannelPlaying) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	s.logAction("PUT", e.Request().URL.Path, c)
	setHtmxTrigger(e)
	return s.jsonChannel(e, c)
}

// --- Target-Specific Channel Actions ---

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
	// If this channel is currently active on the master stream, refresh the live state
	if c.ID == s.Network.Live() {
		_ = s.Network.RepairLive() // RepairLive uses the coordinated refreshLight
	}
	s.logAction("PUT", e.Request().URL.Path, c)
	setHtmxTrigger(e)
	return s.jsonChannel(e, c)
}

// playPrevious advances the specified channel back to its previous media item.
func (s *Server) playPrevious(e echo.Context) error {
	c, err := s.Network.Channel(e.Param("channel_id"))
	if err != nil {
		if errors.Is(err, network.ErrNetworkChannelNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, err.Error())
		}
		return err
	}
	_ = c.PlayPrevious()
	// If this channel is currently active on the master stream, refresh the live state
	if c.ID == s.Network.Live() {
		_ = s.Network.RepairLive()
	}
	s.logAction("PUT", e.Request().URL.Path, c)
	setHtmxTrigger(e)
	return s.jsonChannel(e, c)
}

package domain

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Polypheides/go-homelab-cable/network"
)

type Channel struct {
	ID                  string `json:"id"`
	Number              int    `json:"number"`
	Playing             string `json:"playing"`
	UpNext              string `json:"up_next,omitempty"`
	StreamURL           string `json:"stream_url"`
	HttpStreamURL       string `json:"http_stream_url,omitempty"`
	MasterStreamURL     string `json:"master_stream_url,omitempty"`
	HttpMasterStreamURL string `json:"http_master_stream_url,omitempty"`
	Tuned               bool   `json:"tuned"`        // Is this channel showing on the host VLC window?
	Broadcasting        bool   `json:"broadcasting"` // Is the FFmpeg stream active?
	OverlayText         string `json:"overlay_text,omitempty"`
}

type Network struct {
	Name                string `json:"name"`
	Owner               string `json:"owner"`
	CallSign            string `json:"call_sign"`
	MasterStreamURL     string `json:"master_stream_url"`
	HttpMasterStreamURL string `json:"http_master_stream_url,omitempty"`
}

func ToChannelModel(n *network.Network, c *network.Channel, host string) Channel {
	if host == "" || host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "localhost:") {
		host = network.GetLocalIP() + ":" + n.WebServerPort
	}
	return Channel{
		ID:                  c.ID,
		Number:              c.Number,
		Playing:             filepath.Base(c.Current()),
		UpNext:              filepath.Base(c.UpNext()),
		StreamURL:           c.BroadcastURL(),
		HttpStreamURL:       fmt.Sprintf("http://%s/%d/", host, c.Number),
		MasterStreamURL:     n.MasterStreamURL(),
		HttpMasterStreamURL: fmt.Sprintf("http://%s/master", host),
		Tuned:               n.Live() == c.ID,
		Broadcasting:        true, // In this architecture, added channels are always broadcasting
		OverlayText:         c.OverlayText(),
	}
}

func (c Channel) String() string {
	s := fmt.Sprintf("[CH %d] (%s)\n  Playing: %s\n  Up Next: %s\n  Stream:  %s",
		c.Number, c.ID, c.Playing, c.UpNext, c.StreamURL)
	if c.HttpStreamURL != "" {
		s += fmt.Sprintf("\n  HTTP:    %s", c.HttpStreamURL)
	}
	if c.Tuned {
		s += fmt.Sprintf("\n  Master:  %s  <-- TUNED", c.MasterStreamURL)
		if c.HttpMasterStreamURL != "" {
			s += fmt.Sprintf("\n  Master HTTP: %s", c.HttpMasterStreamURL)
		}
	}
	return s
}
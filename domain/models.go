package domain

import (
	"fmt"
	"path/filepath"

	"github.com/Polypheides/go-homelab-cable/network"
)

type Channel struct {
	ID              string `json:"id"`
	Number          int    `json:"number"`
	Playing         string `json:"playing"`
	UpNext          string `json:"up_next,omitempty"`
	StreamURL       string `json:"stream_url"`
	MasterStreamURL string `json:"master_stream_url,omitempty"`
	Tuned           bool   `json:"tuned"`        // Is this channel showing on the host VLC window?
	Broadcasting    bool   `json:"broadcasting"` // Is the FFmpeg stream active?
}

type Network struct {
	Name            string `json:"name"`
	Owner           string `json:"owner"`
	CallSign        string `json:"call_sign"`
	MasterStreamURL string `json:"master_stream_url"`
}

func ToChannelModel(n *network.Network, c *network.Channel) Channel {
	return Channel{
		ID:              c.ID,
		Number:          c.Number,
		Playing:         filepath.Base(c.Current()),
		UpNext:          filepath.Base(c.UpNext()),
		StreamURL:       c.BroadcastURL(),
		MasterStreamURL: n.MasterStreamURL(),
		Tuned:           n.Live() == c.ID,
		Broadcasting:    true, // In this architecture, added channels are always broadcasting
	}
}

func (c Channel) String() string {
	s := fmt.Sprintf("[CH %d] (%s)\n  Playing: %s\n  Up Next: %s\n  Stream:  %s", 
		c.Number, c.ID, c.Playing, c.UpNext, c.StreamURL)
	if c.Tuned {
		s += fmt.Sprintf("\n  Master:  %s  ← TUNED", c.MasterStreamURL)
	}
	return s
}

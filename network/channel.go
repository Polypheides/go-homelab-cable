package network

import (
	"github.com/Polypheides/go-homelab-cable/player"
	"github.com/google/uuid"
)

type Channel struct {
	ID          string
	Number      int
	list        *player.MediaList
	p           player.Player
	broad       *player.Broadcaster
	stereoOnly  bool
	overlayText string
}

// NewChannel initializes a new broadcast channel and starts its background streaming process.
func NewChannel(list *player.MediaList, broadcasterPort int, number int, protocol string, stereoOnly bool, overlayText string) (*Channel, error) {
	broad := player.NewBroadcaster(list, broadcasterPort)
	broad.Protocol = protocol
	broad.ForceStereo = stereoOnly
	broad.OverlayText = overlayText

	c := &Channel{
		ID:          uuid.New().String(),
		Number:      number,
		list:        list,
		broad:       broad,
		stereoOnly:  stereoOnly,
		overlayText: overlayText,
	}
	if err := c.broad.Start(); err != nil {
		return nil, err
	}
	return c, nil
}

// OverlayText returns the current callsign overlay string for the channel.
func (c *Channel) OverlayText() string {
	return c.overlayText
}

// Season returns the targeted season number for the current media list.
func (c *Channel) Season() int {
	return c.list.Season
}

// SortMode returns the playlist sorting mode (e.g., "E" for Episodic, "R" for Random).
func (c *Channel) SortMode() string {
	return c.list.SortMode
}

// PlayWith initializes a player and starts playback of the channel's media list.
func (c *Channel) PlayWith(p player.Player) error {
	if c.p != nil {
		if err := c.p.Shutdown(); err != nil {
			return err
		}
		c.p = nil
	}

	err := p.Init()
	if err != nil {
		return err
	}

	c.p = p
	return p.Play(c.list)
}

// Broadcaster returns the underlying broadcaster engine for the channel.
func (c *Channel) Broadcaster() *player.Broadcaster {
	return c.broad
}

// UpNext returns the file path of the next media item in the playlist.
func (c *Channel) UpNext() string {
	return c.list.Next()
}

// Current returns the file path of the currently playing media item.
func (c *Channel) Current() string {
	return c.list.Current()
}

// PlayNext manually triggers the broadcaster to skip to the next item in the playlist.
func (c *Channel) PlayNext() string {
	_ = c.broad.Advance()
	return c.Current()
}

// PlayPrevious manually triggers the broadcaster to skip back to the previous item.
func (c *Channel) PlayPrevious() string {
	_ = c.broad.Rewind()
	return c.Current()
}

// BroadcastURL returns the local streaming URL for the channel's broadcast.
func (c *Channel) BroadcastURL() string {
	return c.broad.StreamURL()
}

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
	// Start the background broadcast immediately
	if err := c.broad.Start(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Channel) OverlayText() string {
	return c.overlayText
}

func (c *Channel) Season() int {
	return c.list.Season
}

func (c *Channel) SortMode() string {
	return c.list.SortMode
}

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

func (c *Channel) Broadcaster() *player.Broadcaster {
	return c.broad
}

func (c *Channel) UpNext() string {
	return c.list.Next()
}

func (c *Channel) Current() string {
	return c.list.Current()
}

func (c *Channel) PlayNext() string {
	_ = c.broad.Advance()
	// If the viewer player is active, it will naturally pick up the stream change
	// because it's tuning into the same port.
	return c.Current()
}

func (c *Channel) PlayPrevious() string {
	_ = c.broad.Rewind()
	return c.Current()
}

func (c *Channel) BroadcastURL() string {
	return c.broad.StreamURL()
}

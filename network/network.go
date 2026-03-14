package network

import (
	"errors"
	"sync"

	"github.com/Polypheides/go-homelab-cable/player"
)

var ErrNetworkNoChannelPlaying = errors.New("network no channel playing")
var ErrNetworkChannelNotFound = errors.New("network channel not found")

type Network struct {
	Name          string
	Owner         string
	CallSign      string
	Protocol      string // "udp" or "tcp"
	StereoOnly    bool   // Forces all channels to stereo AC3 (idiot-device mode)
	NoBug         bool
	WebServerPort string

	// Lock order (to prevent deadlocks): always acquire tuneMu before mu.
	mu           sync.RWMutex
	tuneMu       sync.Mutex // Guards channel tuning
	channels     map[string]*Channel
	tunedChannel string                    // The channel which is currently displaying video on the host
	nextPort     int                       // The next available port for a broadcaster
	master       *player.MasterBroadcaster // Port 4999 relay
}

func NewNetwork(name string, owner string, callSign string, protocol string, stereoOnly bool) *Network {
	if name == "" {
		name = "Homelab Cable"
	}
	if owner == "" {
		owner = "clabretro"
	}
	if callSign == "" {
		callSign = "KHLC"
	}
	if protocol == "" {
		protocol = "udp"
	}

	n := &Network{
		Name:          name,
		Owner:         owner,
		CallSign:      callSign,
		Protocol:      protocol,
		StereoOnly:    stereoOnly,
		WebServerPort: "3004", // Default
		channels:      make(map[string]*Channel),
		nextPort:      5000, // Starts at 5000
		master:        player.NewMasterBroadcaster(),
	}
	n.master.Protocol = protocol

	return n
}

func (n *Network) AddChannel(list *player.MediaList) (*Channel, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Derive friendly channel number exactly from the port offset
	// e.g. Port 5000 -> Channel 0, Port 5001 -> Channel 1
	channelNum := n.nextPort - 5000
	callsign := n.CallSign
	if n.NoBug {
		callsign = ""
	}
	c, err := NewChannel(list, n.nextPort, channelNum, n.Protocol, n.StereoOnly, callsign)
	if err != nil {
		return nil, err
	}
	n.nextPort++

	n.channels[c.ID] = c
	return c, nil
}

func (n *Network) Channel(ID string) (*Channel, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if c, ok := n.channels[ID]; ok {
		return c, nil
	}
	return nil, ErrNetworkChannelNotFound
}

func (n *Network) ChannelByNumber(number int) (*Channel, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	for _, c := range n.channels {
		if c.Number == number {
			return c, nil
		}
	}
	return nil, ErrNetworkChannelNotFound
}

func (n *Network) Channels() []*Channel {
	n.mu.RLock()
	defer n.mu.RUnlock()

	channels := make([]*Channel, 0, len(n.channels))
	for _, c := range n.channels {
		channels = append(channels, c)
	}
	return channels
}

func (n *Network) CurrentChannel() (*Channel, error) {
	n.mu.RLock()
	tuned := n.tunedChannel
	n.mu.RUnlock()

	if tuned == "" {
		return nil, ErrNetworkNoChannelPlaying
	}
	return n.Channel(tuned)
}

func (n *Network) SetChannelLive(ID string) error {
	n.tuneMu.Lock()
	defer n.tuneMu.Unlock()

	c, err := n.Channel(ID)
	if err != nil {
		return err
	}

	current, err := n.CurrentChannel()
	if err != nil && !errors.Is(err, ErrNetworkNoChannelPlaying) {
		return err
	}

	if current != nil && current.p != nil {
		// When a channel is no longer live, it just continues broadcasting in the background
		// We don't necessarily need to move it back to a NullPlayer anymore,
		// but we should ensure the player is cleared if needed.
		if err := current.p.Shutdown(); err != nil {
			return err
		}
		current.p = nil
	}

	// For the new live channel, we don't start a whole new playlist,
	// we just "tune in" to its existing broadcast.
	p := player.NewLivePlayer(n.master)
	if err := p.Init(); err != nil {
		return err
	}

	// Make sure the player knows about the list for metadata/skips
	_ = p.Play(c.list)

	if err := p.PlayURL(c.BroadcastURL()); err != nil {
		return err
	}

	c.p = p

	n.mu.Lock()
	n.tunedChannel = c.ID
	n.mu.Unlock()

	// Tune the master relay (port 4999) to this channel's source.
	// Non-fatal: don't abort the tune if the relay fails.
	if err := n.master.Tune(c.BroadcastURL()); err != nil {
		_ = err
	}

	return nil
}

func (n *Network) Live() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.tunedChannel
}

// MasterBroadcaster returns the master relay instance.
func (n *Network) MasterBroadcaster() *player.MasterBroadcaster {
	return n.master
}

// MasterStreamURL returns the fixed URL of the master relay port (4999).
func (n *Network) MasterStreamURL() string {
	return player.MasterStreamURL(n.Protocol)
}

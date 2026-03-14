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
	Protocol      string
	StereoOnly    bool
	NoBug         bool
	WebServerPort string
	mu           sync.RWMutex
	tuneMu       sync.Mutex
	channels     map[string]*Channel
	tunedChannel string
	nextPort     int
	master       *player.MasterBroadcaster
}

// NewNetwork creates a new network manager with the specified identity and preferences.
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
		WebServerPort: "3004",
		channels:      make(map[string]*Channel),
		nextPort:      5000,
		master:        player.NewMasterBroadcaster(),
	}
	n.master.Protocol = protocol

	return n
}

// AddChannel registers a new media list as a channel on the network.
func (n *Network) AddChannel(list *player.MediaList) (*Channel, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

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

// Channel retrieves a channel by its unique identifier.
func (n *Network) Channel(ID string) (*Channel, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if c, ok := n.channels[ID]; ok {
		return c, nil
	}
	return nil, ErrNetworkChannelNotFound
}

// ChannelByNumber retrieves a channel by its friendly number.
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

// Channels returns a slice of all registered channels.
func (n *Network) Channels() []*Channel {
	n.mu.RLock()
	defer n.mu.RUnlock()

	channels := make([]*Channel, 0, len(n.channels))
	for _, c := range n.channels {
		channels = append(channels, c)
	}
	return channels
}

// CurrentChannel returns the currently tuned live channel on the network.
func (n *Network) CurrentChannel() (*Channel, error) {
	n.mu.RLock()
	tuned := n.tunedChannel
	n.mu.RUnlock()

	if tuned == "" {
		return nil, ErrNetworkNoChannelPlaying
	}
	return n.Channel(tuned)
}

// SetChannelLive tunes the network's master relay to the specified channel ID.
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
		if err := current.p.Shutdown(); err != nil {
			return err
		}
		current.p = nil
	}

	p := player.NewLivePlayer(n.master)
	if err := p.Init(); err != nil {
		return err
	}

	_ = p.Play(c.list)

	if err := p.PlayURL(c.BroadcastURL()); err != nil {
		return err
	}

	c.p = p

	n.mu.Lock()
	n.tunedChannel = c.ID
	n.mu.Unlock()

	if err := n.master.Tune(c.BroadcastURL()); err != nil {
		_ = err
	}

	return nil
}

// Live returns the ID of the currently tuned channel.
func (n *Network) Live() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.tunedChannel
}

// MasterBroadcaster returns the master relay instance.
func (n *Network) MasterBroadcaster() *player.MasterBroadcaster {
	return n.master
}

// MasterStreamURL returns the fixed URL of the master relay.
func (n *Network) MasterStreamURL() string {
	return player.MasterStreamURL(n.Protocol)
}

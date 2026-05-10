package network

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Polypheides/go-homelab-cable/logger"
	"github.com/Polypheides/go-homelab-cable/player"
)

var Log = logger.For("network")
var ErrorLog = logger.For("error")

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
	mu            sync.RWMutex
	tuneMu        sync.Mutex
	channels      map[string]*Channel
	tunedChannel  string
	lastTunedID   string
	nextPort      int
	master        *player.MasterBroadcaster
	livePlayer    player.Player
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
	callsign := parseCallsign(n.CallSign, channelNum)

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

// AddChannelFromPath discovers media at the specified path and registers it as a new network channel.
func (n *Network) AddChannelFromPath(path string, season int, mode string) (*Channel, error) {
	var strategy player.MediaListSortStrategy
	if mode == "r" {
		strategy = player.SortStratRandom{}
	} else {
		strategy = player.SortStratAlphabetical{}
	}

	list, err := player.FromFolderWithSeason(path, strategy, season)
	if err != nil {
		return nil, err
	}

	return n.AddChannel(list)
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

	currentID := n.Live()
	if currentID != "" && currentID != ID {
		n.mu.Lock()
		n.lastTunedID = currentID
		n.mu.Unlock()
	}

	if n.livePlayer == nil {
		n.livePlayer = player.NewLivePlayer(n.master)
		if err := n.livePlayer.Init(); err != nil {
			return err
		}
	}

	n.mu.Lock()
	n.tunedChannel = c.ID
	n.mu.Unlock()

	// 1. Tune the master relay FIRST so it's ready for the player
	if err := n.master.Tune(c.BroadcastURL()); err != nil {
		_ = err
	}

	// 2. Start/Switch the player AFTER the master is ready (with a slight delay for FFmpeg to warm up)
	masterURL := n.MasterStreamURL()
	go func() {
		// Wait for both the broadcaster and the master relay to have data flowing
		time.Sleep(2 * time.Second)
		_ = n.livePlayer.PlayURL(masterURL)
	}()

	return nil
}

// RecallLive switches to the previously tuned channel.
func (n *Network) RecallLive() error {
	n.mu.RLock()
	prev := n.lastTunedID
	n.mu.RUnlock()

	if prev == "" {
		return errors.New("no recall history")
	}
	return n.SetChannelLive(prev)
}

// PlayLiveNext advances the currently tuned live channel to its next media item and refreshes the master relay.
func (n *Network) PlayLiveNext() (*Channel, error) {
	c, err := n.CurrentChannel()
	if err != nil {
		return nil, err
	}
	_ = c.PlayNext()
	return c, n.refreshLive(c)
}

// PlayLivePrevious rewinds the currently tuned live channel to its previous media item and refreshes the master relay.
func (n *Network) PlayLivePrevious() (*Channel, error) {
	c, err := n.CurrentChannel()
	if err != nil {
		return nil, err
	}
	_ = c.PlayPrevious()
	return c, n.refreshLive(c)
}

// refreshLive performs a coordinated reset of the master relay and local player for the given channel.
func (n *Network) refreshLive(c *Channel) error {
	// Give the broadcaster's new FFmpeg process a moment to start its listener
	time.Sleep(1 * time.Second)

	// Hard stop the master to clear hung listeners
	_ = n.master.Stop()

	// Cycle the player if it exists to ensure a fresh connection
	if n.livePlayer != nil {
		_ = n.livePlayer.Shutdown()
		_ = n.livePlayer.Init()
	}

	// Re-tune the master to the newly started source
	if err := n.master.Tune(c.BroadcastURL()); err != nil {
		return err
	}

	// Restart player if present (with delay for master FFmpeg to stabilize)
	if n.livePlayer != nil {
		masterURL := n.MasterStreamURL()
		go func() {
			time.Sleep(2 * time.Second)
			_ = n.livePlayer.PlayURL(masterURL)
		}()
	}

	return nil
}

// RepairLive restarts the currently tuned channel and master relay.
func (n *Network) RepairLive() error {
	c, err := n.CurrentChannel()
	if err != nil {
		return err
	}

	ErrorLog.Printf("[REPAIR] [Network] Performing Forced Repair on CH %d...\n", c.Number)

	if err := c.Repair(); err != nil {
		return err
	}

	return n.refreshLive(c)
}

// RepairAll restarts every active channel on the network.
func (n *Network) RepairAll() error {
	ErrorLog.Printf("[REPAIR] [Network] Performing Universal Network Repair (All Channels)...\n")
	for _, c := range n.Channels() {
		_ = c.Repair()
	}

	// Hard stop the master to clear hung listeners
	_ = n.master.Stop()

	// Also re-kick the master and player if something is playing
	if curr, err := n.CurrentChannel(); err == nil {
		// Cycle the player if it exists to ensure a fresh connection
		if n.livePlayer != nil {
			_ = n.livePlayer.Shutdown()
			_ = n.livePlayer.Init()
		}

		// Re-tune the master to the newly started source
		if err := n.master.Tune(curr.BroadcastURL()); err != nil {
			return err
		}

		// Restart player if present
		if n.livePlayer != nil {
			_ = n.livePlayer.PlayURL(n.MasterStreamURL())
		}
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

// Stop gracefully shuts down all active channels and the master relay on the network in parallel.
func (n *Network) Stop() {
	n.mu.RLock()
	defer n.mu.RUnlock()

	var wg sync.WaitGroup

	// Stop channels in parallel
	for _, c := range n.channels {
		if c.broad != nil {
			wg.Add(1)
			go func(b *player.Broadcaster) {
				defer wg.Done()
				_ = b.Stop()
			}(c.broad)
		}
	}

	// Stop master in parallel
	if n.master != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = n.master.Stop()
		}()
	}

	// Stop player in parallel if it exists
	if n.livePlayer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = n.livePlayer.Shutdown()
		}()
	}

	wg.Wait()
}

// parseCallsign processes the callsign string, handling the # placeholder.
func parseCallsign(raw string, channelNum int) string {
	// Standalone # always gets replaced for convenience
	if raw == "#" {
		return strconv.Itoa(channelNum)
	}

	parts := strings.Split(raw, ":")
	// If the last part is our placeholder, we treat everything before the last colon as the name.
	// This ensures that literal hashtags in the name (e.g. #STATION:#) are preserved.
	if len(parts) > 1 && parts[len(parts)-1] == "#" {
		// Join everything BEFORE the last colon
		name := strings.Join(parts[:len(parts)-1], ":")
		return name + strconv.Itoa(channelNum)
	}

	// In all other cases, return the raw callsign (allowing literal hashtags)
	return raw
}

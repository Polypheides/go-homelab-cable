//go:build !vlc

package player

import (
	"time"
)

// NewLivePlayer returns a NullPlayer when VLC is not enabled on the system.
func NewLivePlayer(master *MasterBroadcaster) Player {
	return &NullPlayer{}
}

// NullPlayer provides a headless implementation that advances the playlist without media output.
type NullPlayer struct {
	list   *MediaList
	ticker *time.Ticker
	done   chan bool
}

// Init prepares the null player for background operation.
func (n *NullPlayer) Init() error {
	return nil
}

// Play starts the background simulation of media consumption.
func (n *NullPlayer) Play(list *MediaList) error {
	n.list = list
	if n.ticker != nil {
		return nil
	}
	n.ticker = time.NewTicker(time.Minute * 30)
	n.done = make(chan bool)
	go func() {
		for {
			select {
			case <-n.done:
				return
			case <-n.ticker.C:
				n.PlayNext()
			}
		}
	}()
	return nil
}

// PlayURL is a no-op for the null player.
func (n *NullPlayer) PlayURL(url string) error {
	return nil
}

// PlayNext advances the underlying media list.
func (n *NullPlayer) PlayNext() error {
	if n.list != nil {
		n.list.Advance()
	}
	return nil
}

// PlayPrevious rewinds the underlying media list.
func (n *NullPlayer) PlayPrevious() error {
	if n.list != nil {
		n.list.Rewind()
	}
	return nil
}

// Next returns the file path of the next item in the media list.
func (n *NullPlayer) Next() string {
	if n.list != nil {
		return n.list.Next()
	}
	return ""
}

// Current returns the file path of the current item in the media list.
func (n *NullPlayer) Current() string {
	if n.list != nil {
		return n.list.Current()
	}
	return ""
}

// Shutdown terminates the null player's background activity.
func (n *NullPlayer) Shutdown() error {
	if n.ticker != nil {
		n.ticker.Stop()
	}
	if n.done != nil {
		select {
		case n.done <- true:
		default:
		}
	}
	return nil
}

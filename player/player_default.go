//go:build !vlc

package player

// NewLivePlayer returns a NullPlayer by default when VLC is not enabled.
func NewLivePlayer() Player {
	return &NullPlayer{}
}

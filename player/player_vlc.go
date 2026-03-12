//go:build vlc

package player

// NewLivePlayer returns a VLCPlayer when the 'vlc' build tag is provided.
func NewLivePlayer() Player {
	return &VLCPlayer{}
}

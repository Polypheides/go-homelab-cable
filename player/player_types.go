package player

import "errors"

// Player defines the interface for media playback engines (e.g., VLC, Headless).
type Player interface {
	Init() error
	Play(list *MediaList) error
	PlayURL(url string) error
	PlayNext() error
	PlayPrevious() error
	Next() string
	Current() string
	Shutdown() error
}

var ErrNoMoreMedia = errors.New("no more media in the list")
var ErrPlayerNotInitialized = errors.New("player wasn't initialized")

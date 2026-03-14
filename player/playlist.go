package player

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
)

type MediaListSortStrategy interface {
	Sort([]string)
}

type SortStratRandom struct{}

// Sort shuffles the provided file list randomly.
func (s SortStratRandom) Sort(list []string) {
	rand.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
}

type SortStratAlphabetical struct{}

// Sort organizes the provided file list in alphabetical order.
func (s SortStratAlphabetical) Sort(list []string) {
	sort.Strings(list)
}

type MediaList struct {
	list         []string
	nextList     []string
	current      int
	SortStrategy MediaListSortStrategy
	Season       int
	SortMode     string // "E" or "R"

	mu sync.Mutex
}

// NewMediaList initializes a new media list with the given files and sorting strategy.
func NewMediaList(list []string, sortStrat MediaListSortStrategy) (*MediaList, error) {
	if len(list) == 0 {
		return nil, errors.New("need media")
	}
	ml := &MediaList{
		list:         list,
		SortStrategy: sortStrat,
		nextList:     make([]string, len(list)),
	}
	copy(ml.nextList, list)
	ml.SortStrategy.Sort(ml.list)
	ml.SortStrategy.Sort(ml.nextList)
	return ml, nil
}

// All returns a shallow copy of all media file paths in the list.
func (ml *MediaList) All() []string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	out := make([]string, len(ml.list))
	copy(out, ml.list)
	return out
}

// Snapshot returns a copy of the playlist and the currently active file atomically.
func (ml *MediaList) Snapshot() ([]string, string) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	out := make([]string, len(ml.list))
	copy(out, ml.list)
	return out, ml.list[ml.current]
}

// Current returns the file path of the media item currently selected.
func (ml *MediaList) Current() string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	return ml.list[ml.current]
}

// Next returns the file path of the media item that follows the current one.
func (ml *MediaList) Next() string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.current+1 >= len(ml.list) {
		return ml.nextList[0]
	}
	return ml.list[ml.current+1]
}

// Advance moves the selection to the next item, cycling back to the start if necessary.
func (ml *MediaList) Advance() string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.current+1 >= len(ml.list) {
		ml.list, ml.nextList = ml.nextList, ml.list
		ml.SortStrategy.Sort(ml.nextList)
		ml.current = 0
	} else {
		ml.current++
	}
	return ml.list[ml.current]
}

// Rewind moves the selection to the previous item, cycling back to the end if necessary.
func (ml *MediaList) Rewind() string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.current-1 < 0 {
		ml.current = len(ml.list) - 1
	} else {
		ml.current--
	}
	return ml.list[ml.current]
}

var VideoFiles map[string]struct{} = map[string]struct{}{
	".avi": {},
	".mp4": {},
	".mkv": {},
}

// FromFolder scans a directory for video files and initializes a media list.
func FromFolder(folderPath string, sortStrat MediaListSortStrategy) (*MediaList, error) {
	return FromFolderWithSeason(folderPath, sortStrat, 0)
}

// FromFolderWithSeason scans a directory for videos belonging to a specific season.
func FromFolderWithSeason(folderPath string, sortStrat MediaListSortStrategy, targetSeason int) (*MediaList, error) {
	var paths []string
	if err := filepath.Walk(folderPath, func(file string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if _, ok := VideoFiles[filepath.Ext(file)]; ok {
			if targetSeason > 0 && !matchesSeason(file, targetSeason) {
				return nil
			}
			paths = append(paths, file)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	ml, err := NewMediaList(paths, sortStrat)
	if err != nil {
		return nil, err
	}
	ml.Season = targetSeason
	if _, ok := sortStrat.(SortStratAlphabetical); ok {
		ml.SortMode = "E"
	} else {
		ml.SortMode = "R"
	}
	return ml, nil
}

// matchesSeason use regex to determine if a file path corresponds to a given season number.
func matchesSeason(path string, target int) bool {
	pattern := fmt.Sprintf(`(?i)(season\s*|s|s\.)0*%d(?:[^0-9]|$)`, target)
	matched, _ := regexp.MatchString(pattern, path)
	return matched
}

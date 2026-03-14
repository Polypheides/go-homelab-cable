package player

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

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

// --- Media Management ---

type MediaListSortStrategy interface {
	Sort([]string)
}

type SortStratRandom struct{}

func (s SortStratRandom) Sort(list []string) {
	rand.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
}

type SortStratAlphabetical struct{}

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

func (ml *MediaList) All() []string {
	return ml.list
}

func (ml *MediaList) Current() string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	return ml.list[ml.current]
}

func (ml *MediaList) Next() string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.current+1 >= len(ml.list) {
		return ml.nextList[0]
	}
	return ml.list[ml.current+1]
}

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

func (ml *MediaList) Rewind() string {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if ml.current-1 < 0 {
		// Just loop to the end of the current list for simplicity
		// (In a more complex app we might want to swap back to the previous list,
		// but since it's reshuffled, "previous" is relative).
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

func FromFolder(folderPath string, sortStrat MediaListSortStrategy) (*MediaList, error) {
	return FromFolderWithSeason(folderPath, sortStrat, 0)
}

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

func matchesSeason(path string, target int) bool {
	// Boundary check regex: Match "season X", "sX", or "s.X" where X is the target.
	// Ensure X is not followed by another digit (so S1 don't match S10).
	pattern := fmt.Sprintf(`(?i)(season\s*|s|s\.)0*%d(?:[^0-9]|$)`, target)
	matched, _ := regexp.MatchString(pattern, path)
	return matched
}

// --- Broadcasting ---

const MasterPort = 4999

func formatOutputURL(protocol string, port int, isListen bool) string {
	if protocol == "tcp" {
		url := fmt.Sprintf("tcp://127.0.0.1:%d", port)
		if isListen {
			url += "?listen"
		}
		return url
	}
	// UDP default
	return fmt.Sprintf("udp://127.0.0.1:%d?pkt_size=1316", port)
}

func formatListenURL(protocol string, port int) string {
	if protocol == "tcp" {
		return fmt.Sprintf("tcp://127.0.0.1:%d", port)
	}
	return fmt.Sprintf("udp://@127.0.0.1:%d", port)
}

// Broadcaster manages a background FFmpeg process that streams a media list.
type Broadcaster struct {
	list         *MediaList
	port         int
	Protocol     string // "udp" or "tcp"
	cmd          *exec.Cmd
	playlistFile string

	// TCP relay support
	mu    sync.Mutex
	conns map[net.Conn]struct{}
	l     net.Listener
}

func NewBroadcaster(list *MediaList, port int) *Broadcaster {
	return &Broadcaster{
		list:     list,
		port:     port,
		Protocol: "udp", // default
		conns:    make(map[net.Conn]struct{}),
	}
}

func (b *Broadcaster) Init() error {
	tmpDir := os.TempDir()
	b.playlistFile = filepath.Join(tmpDir, fmt.Sprintf("cable_playlist_%d.txt", b.port))
	return b.updatePlaylist()
}

func (b *Broadcaster) updatePlaylist() error {
	var sb strings.Builder
	all := b.list.All()
	currentIdx := 0
	currentFile := b.list.Current()

	for i, f := range all {
		if f == currentFile {
			currentIdx = i
			break
		}
	}

	for i := 0; i < len(all); i++ {
		idx := (currentIdx + i) % len(all)
		file := all[idx]
		absPath, err := filepath.Abs(file)
		if err != nil {
			return err
		}
		cleanPath := filepath.ToSlash(absPath)
		fmt.Fprintf(&sb, "file '%s'\n", cleanPath)
	}

	return os.WriteFile(b.playlistFile, []byte(sb.String()), 0644)
}

func (b *Broadcaster) Start() error {
	if b.playlistFile == "" {
		if err := b.Init(); err != nil {
			return err
		}
	}

	outputURL := ""
	if b.Protocol == "tcp" {
		outputURL = "-" // output to stdout
		if b.l == nil {
			var err error
			b.l, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", b.port))
			if err != nil {
				return err
			}
			go b.acceptLoop()
		}
	} else {
		outputURL = formatOutputURL(b.Protocol, b.port, true)
	}

	args := []string{
		"-re",
		"-fflags", "+genpts+igndts",
		"-avoid_negative_ts", "make_zero",
		"-f", "concat",
		"-safe", "0",
		"-stream_loop", "-1",
		"-i", b.playlistFile,
		"-map", "0",
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", "192k",
		"-af", "aresample=async=1:min_hard_comp=1",
		"-f", "mpegts",
		"-mpegts_flags", "resend_headers",
		"-muxdelay", "0",
		"-y", outputURL,
	}

	b.cmd = exec.Command("ffmpeg", args...)

	if b.Protocol == "tcp" {
		stdout, err := b.cmd.StdoutPipe()
		if err != nil {
			return err
		}
		go b.relayLoop(stdout)
	}

	return b.cmd.Start()
}

func (b *Broadcaster) acceptLoop() {
	for {
		conn, err := b.l.Accept()
		if err != nil {
			return
		}
		b.mu.Lock()
		b.conns[conn] = struct{}{}
		b.mu.Unlock()
	}
}

func (b *Broadcaster) relayLoop(r io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			b.mu.Lock()
			for conn := range b.conns {
				_, err := conn.Write(buf[:n])
				if err != nil {
					conn.Close()
					delete(b.conns, conn)
				}
			}
			b.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (b *Broadcaster) Stop() error {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_ = b.cmd.Wait()
		b.cmd = nil
	}
	if b.l != nil {
		_ = b.l.Close()
		b.l = nil
	}
	b.mu.Lock()
	for conn := range b.conns {
		_ = conn.Close()
	}
	b.conns = make(map[net.Conn]struct{})
	b.mu.Unlock()

	if b.playlistFile != "" {
		_ = os.Remove(b.playlistFile)
	}
	return nil
}

func (b *Broadcaster) Advance() error {
	b.list.Advance()
	if err := b.updatePlaylist(); err != nil {
		return err
	}
	// In TCP mode, we keep the listener alive, just restart the source FFmpeg
	if b.Protocol == "tcp" {
		if b.cmd != nil && b.cmd.Process != nil {
			_ = b.cmd.Process.Kill()
			_ = b.cmd.Wait()
			b.cmd = nil
		}
		return b.Start()
	}
	_ = b.Stop()
	return b.Start()
}

func (b *Broadcaster) Rewind() error {
	b.list.Rewind()
	if err := b.updatePlaylist(); err != nil {
		return err
	}
	if b.Protocol == "tcp" {
		if b.cmd != nil && b.cmd.Process != nil {
			_ = b.cmd.Process.Kill()
			_ = b.cmd.Wait()
			b.cmd = nil
		}
		return b.Start()
	}
	_ = b.Stop()
	return b.Start()
}

func (b *Broadcaster) StreamURL() string {
	return formatListenURL(b.Protocol, b.port)
}

type MasterBroadcaster struct {
	cmd       *exec.Cmd
	sourceURL string
	Protocol  string // "udp" or "tcp"

	// TCP relay support
	mu    sync.Mutex
	conns map[net.Conn]struct{}
	l     net.Listener
}

func NewMasterBroadcaster() *MasterBroadcaster {
	return &MasterBroadcaster{
		Protocol: "udp", // default
		conns:    make(map[net.Conn]struct{}),
	}
}

func (m *MasterBroadcaster) Tune(sourceURL string) error {
	// In TCP mode, we only stop the source FFmpeg, not the whole listener
	if m.Protocol == "tcp" {
		if m.cmd != nil && m.cmd.Process != nil {
			_ = m.cmd.Process.Kill()
			_ = m.cmd.Wait()
			m.cmd = nil
		}
	} else {
		_ = m.Stop()
	}

	time.Sleep(500 * time.Millisecond)
	m.sourceURL = sourceURL
	return m.start()
}

func (m *MasterBroadcaster) start() error {
	if m.sourceURL == "" {
		return nil
	}

	outputURL := ""
	if m.Protocol == "tcp" {
		outputURL = "-" // stdout
		if m.l == nil {
			var err error
			m.l, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", MasterPort))
			if err != nil {
				return err
			}
			go m.acceptLoop()
		}
	} else {
		outputURL = formatOutputURL(m.Protocol, MasterPort, true)
	}

	args := []string{
		"-fflags", "+genpts+discardcorrupt",
		"-i", m.sourceURL,
		"-c", "copy",
		"-f", "mpegts",
		"-mpegts_flags", "resend_headers",
		"-y", outputURL,
	}

	m.cmd = exec.Command("ffmpeg", args...)

	if m.Protocol == "tcp" {
		stdout, err := m.cmd.StdoutPipe()
		if err != nil {
			return err
		}
		go m.relayLoop(stdout)
	}

	return m.cmd.Start()
}

func (m *MasterBroadcaster) acceptLoop() {
	for {
		conn, err := m.l.Accept()
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conns[conn] = struct{}{}
		m.mu.Unlock()
	}
}

func (m *MasterBroadcaster) relayLoop(r io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			m.mu.Lock()
			for conn := range m.conns {
				_, err := conn.Write(buf[:n])
				if err != nil {
					conn.Close()
					delete(m.conns, conn)
				}
			}
			m.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (m *MasterBroadcaster) Stop() error {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_ = m.cmd.Wait()
		m.cmd = nil
	}
	if m.l != nil {
		_ = m.l.Close()
		m.l = nil
	}
	m.mu.Lock()
	for conn := range m.conns {
		_ = conn.Close()
	}
	m.conns = make(map[net.Conn]struct{})
	m.mu.Unlock()
	return nil
}

func MasterStreamURL(protocol string) string {
	return formatListenURL(protocol, MasterPort)
}

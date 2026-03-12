package player

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Broadcaster manages a background FFmpeg process that streams a media list
// to a local UDP port.
type Broadcaster struct {
	list         *MediaList
	port         int
	cmd          *exec.Cmd
	playlistFile string
}

func NewBroadcaster(list *MediaList, port int) *Broadcaster {
	return &Broadcaster{
		list: list,
		port: port,
	}
}

func (b *Broadcaster) Init() error {
	// Create a temporary playlist file for FFmpeg's concat demuxer
	tmpDir := os.TempDir()
	b.playlistFile = filepath.Join(tmpDir, fmt.Sprintf("cable_playlist_%d.txt", b.port))

	return b.updatePlaylist()
}

func (b *Broadcaster) updatePlaylist() error {
	var sb strings.Builder
	all := b.list.All()
	currentIdx := 0
	currentFile := b.list.Current()

	// Find current index in the full list
	for i, f := range all {
		if f == currentFile {
			currentIdx = i
			break
		}
	}

	// Write playlist starting from current, then wrapping around
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

	// -re: read at native frame rate
	// -fflags +genpts: generate timestamps if missing
	// -f concat: use the concat demuxer
	// -safe 0: allow absolute paths
	// -stream_loop -1: loop the input indefinitely
	// -i: input playlist
	// -map 0: include all streams
	// -c:v copy: copy video directly
	// -c:a aac: transcode audio (more stable for stream)
	// -b:a 192k: higher bitrate for quality
	// -af "aresample=async=1": fix audio sync
	// -f mpegts: stream format
	// -mpegts_flags resend_headers: help VLC pick up the stream late
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
		fmt.Sprintf("udp://127.0.0.1:%d?pkt_size=1316", b.port),
	}

	b.cmd = exec.Command("ffmpeg", args...)
	
	// Start the process in the background
	return b.cmd.Start()
}

func (b *Broadcaster) Stop() error {
	if b.cmd != nil && b.cmd.Process != nil {
		if err := b.cmd.Process.Kill(); err != nil {
			return err
		}
	}
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
	// Restart FFmpeg to pick up the new start point
	_ = b.Stop()
	return b.Start()
}

func (b *Broadcaster) StreamURL() string {
	return fmt.Sprintf("udp://@127.0.0.1:%d", b.port)
}

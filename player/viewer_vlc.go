//go:build vlc

package player

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// NewLivePlayer returns a new VLC-based player instance.
func NewLivePlayer(master *MasterBroadcaster) Player {
	return &VLCPlayer{master: master}
}

type VLCPlayer struct {
	list     *MediaList
	master   *MasterBroadcaster
	cmd      *exec.Cmd
	done     chan struct{}
	shutMu   sync.Mutex
	shutOnce sync.Once
}

// Init prepares the VLC player for media playback.
func (p *VLCPlayer) Init() error {
	p.shutMu.Lock()
	defer p.shutMu.Unlock()
	p.done = make(chan struct{})
	p.shutOnce = sync.Once{}
	return nil
}

// Shutdown terminates the VLC process and releases all resources.
func (p *VLCPlayer) Shutdown() error {
	p.shutOnce.Do(func() {
		p.shutMu.Lock()
		if p.done != nil {
			close(p.done)
		}
		p.shutMu.Unlock()
	})

	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		p.cmd = nil
	}
	return nil
}

// Play starts media playback for the provided media list.
func (p *VLCPlayer) Play(list *MediaList) error {
	p.list = list
	return p.PlayURL(p.list.Current())
}

// findVLCBinary searches the system for the VLC executable path.
func findVLCBinary() string {
	if path, err := exec.LookPath("vlc"); err == nil {
		return path
	}

	var fallbackPaths []string
	if runtime.GOOS == "windows" {
		fallbackPaths = []string{
			`vlc.exe`,
			`C:\Program Files\VideoLAN\VLC\vlc.exe`,
			`C:\Program Files (x86)\VideoLAN\VLC\vlc.exe`,
		}
	} else {
		fallbackPaths = []string{
			"/usr/bin/vlc",
			"/snap/bin/vlc",
			"/var/lib/flatpak/app/org.videolan.VLC/current/active/files/bin/vlc",
		}
	}

	for _, p := range fallbackPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// findFFplayBinary searches the system and local bin/ for ffplay.
func findFFplayBinary() string {
	if path, err := exec.LookPath("ffplay"); err == nil {
		return path
	}

	// Check local bin/
	localPath := filepath.Join("bin", "ffplay")
	if runtime.GOOS == "windows" {
		localPath += ".exe"
	}

	if _, err := os.Stat(localPath); err == nil {
		abs, _ := filepath.Abs(localPath)
		return abs
	}

	return ""
}

// PlayURL launches a VLC process to play the specified master stream URL.
func (p *VLCPlayer) PlayURL(url string) error {
	masterURL := MasterStreamURL(p.master.Protocol)

	if p.cmd != nil && p.cmd.Process != nil && p.cmd.ProcessState == nil {
		// If we are already playing the master stream, don't restart!
		if url == masterURL {
			return nil
		}
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		p.cmd = nil
	}

	bin := findVLCBinary()

	if bin == "" {
		// Fallback to ffplay as specified in the plan
		if ffplay := findFFplayBinary(); ffplay != "" {
			Log.Printf("[Player] VLC not found. Falling back to built-in ffplay...\n")
			bin = ffplay
		} else {
			Log.Printf("[Player] Neither VLC nor ffplay found on host system. Operating in headless mode tracking stream: %s\n", masterURL)
			return nil
		}
	}

	var args []string
	if strings.Contains(strings.ToLower(bin), "ffplay") {
		args = []string{
			"-window_title", "GoCable Master Stream (ffplay)",
			"-loglevel", "error",
			"-fs",
			masterURL,
		}
	} else {
		args = []string{
			"--fullscreen",
			"--no-video-title-show",
			"--video-on-top",
			"--qt-minimal-view",
			"--no-qt-error-dialogs",
			"--no-osd",
			"--quiet",
			masterURL,
		}
	}

	p.cmd = exec.Command(bin, args...)

	err := p.cmd.Start()
	if err != nil {
		Log.Printf("[Player] Failed to launch VLC process securely. Falling back to headless execution: %v\n", err)
		p.cmd = nil
	} else {
		// Watch the process in the background so we know if it exits
		go func() {
			_ = p.cmd.Wait()
		}()
	}

	return nil
}

// PlayNext advances media and restarts VLC with the new master stream.
func (p *VLCPlayer) PlayNext() error {
	if p.list == nil {
		return nil
	}
	return p.PlayURL(p.list.Advance())
}

// PlayPrevious rewinds media and restarts VLC with the new master stream.
func (p *VLCPlayer) PlayPrevious() error {
	if p.list == nil {
		return nil
	}
	return p.PlayURL(p.list.Rewind())
}

// Next returns the file path of the next item in the media list.
func (p *VLCPlayer) Next() string {
	if p.list == nil {
		return ""
	}
	return p.list.Next()
}

// Current returns the file path of the current item in the media list.
func (p *VLCPlayer) Current() string {
	if p.list == nil {
		return ""
	}
	return p.list.Current()
}

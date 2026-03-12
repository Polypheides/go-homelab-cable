package player

import (
	"fmt"
	"os/exec"
	"time"
)

const MasterPort = 4999

// MasterBroadcaster listens on a fixed "master" port (4999) and re-streams
// whatever UDP source is currently tuned. Switching channels kills the current
// FFmpeg process and restarts it pointing at the new source port.
type MasterBroadcaster struct {
	cmd       *exec.Cmd
	sourceURL string // e.g. udp://@127.0.0.1:5001
}

func NewMasterBroadcaster() *MasterBroadcaster {
	return &MasterBroadcaster{}
}

// Tune switches the master port to re-stream a different source URL.
// It kills the current FFmpeg process and starts a fresh one.
func (m *MasterBroadcaster) Tune(sourceURL string) error {
	_ = m.Stop()

	// Wait for the OS to fully release the source UDP socket from the
	// killed process. Without this, rapid switches (e.g. CH0→CH1→CH0)
	// cause the new FFmpeg to fail to bind because the old socket is
	// still in TIME_WAIT / close_wait state.
	time.Sleep(500 * time.Millisecond)

	m.sourceURL = sourceURL
	return m.start()
}

func (m *MasterBroadcaster) start() error {
	if m.sourceURL == "" {
		return nil
	}

	args := []string{
		"-fflags", "+genpts+discardcorrupt",
		"-i", m.sourceURL,
		"-c", "copy",
		"-f", "mpegts",
		"-mpegts_flags", "resend_headers",
		fmt.Sprintf("udp://127.0.0.1:%d?pkt_size=1316", MasterPort),
	}

	m.cmd = exec.Command("ffmpeg", args...)
	return m.cmd.Start()
}

// Stop kills the currently running FFmpeg relay process and waits for it
// to fully exit so the OS releases its socket resources.
func (m *MasterBroadcaster) Stop() error {
	if m.cmd != nil && m.cmd.Process != nil {
		// Kill the process
		_ = m.cmd.Process.Kill()
		// Wait for full exit so the OS can reclaim the UDP socket
		_ = m.cmd.Wait()
		m.cmd = nil
	}
	return nil
}

// MasterStreamURL returns the fixed listen URL clients connect to.
func MasterStreamURL() string {
	return fmt.Sprintf("udp://@127.0.0.1:%d", MasterPort)
}

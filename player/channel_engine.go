package player

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Broadcaster manages a background FFmpeg process that streams a media list.
type Broadcaster struct {
	list         *MediaList
	port         int
	Protocol     string
	cmd          *exec.Cmd
	playlistFile string
	OverlayText  string
	mu           sync.Mutex
	conns        map[net.Conn]*streamClient
	l            net.Listener
	hub          *StreamHub
	relayDone    chan struct{}
	stopMu       sync.Mutex
	audioMeta    *AudioMetadata
	ForceStereo  bool
	udpConn      *net.UDPConn
	// CRITICAL: Required for robust textfile callsign rendering (e.g. P'''''NET:#)
	OverlayFile string
}

type streamClient struct {
	conn net.Conn
	pos  int64 // Position in the ring buffer
}

// NewBroadcaster creates a new broadcast engine for the specified media list and port.
func NewBroadcaster(list *MediaList, port int) *Broadcaster {
	return &Broadcaster{
		list:     list,
		port:     port,
		Protocol: "udp", // default
		conns:    make(map[net.Conn]*streamClient),
		hub:      NewStreamHub(16384), // 16k chunks (~4-5 seconds) safety net for high-bitrate video bursts
	}
}

// Init sets up the temporary playlist file and prepares the broadcaster for starting.
func (b *Broadcaster) Init() error {
	tmpDir := os.TempDir()
	b.playlistFile = filepath.Join(tmpDir, fmt.Sprintf("cable_playlist_%d.txt", b.port))
	return b.updatePlaylist()
}

// updatePlaylist generates an FFmpeg-compatible concat playlist from the media list.
func (b *Broadcaster) updatePlaylist() error {
	var sb strings.Builder
	all, currentFile := b.list.Snapshot()
	currentIdx := 0

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
		// Escape single quotes for FFmpeg concat demuxer using the triplet method
		escapedPath := strings.ReplaceAll(cleanPath, "'", "'\\''")
		fmt.Fprintf(&sb, "file '%s'\n", escapedPath)
	}

	return os.WriteFile(b.playlistFile, []byte(sb.String()), 0644)
}

// Start spawns the FFmpeg process and begins relaying the stream to clients.
func (b *Broadcaster) Start() error {
	if b.playlistFile == "" {
		if err := b.Init(); err != nil {
			return err
		}
	}

	outputURL := "-"

	switch b.Protocol {
	case "tcp", "http":
		if b.l == nil {
			var err error
			b.l, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", b.port))
			if err != nil {
				return err
			}
			go b.acceptLoop()
		}
	case "udp":
		if b.udpConn == nil {
			addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", b.port))
			if err != nil {
				return err
			}
			b.udpConn, err = net.DialUDP("udp", nil, addr)
			if err != nil {
				return err
			}
		}
	}

	inputFlags, outputFlags := GetProtocolFlags(b.Protocol, false)

	args := []string{
		"-re",
	}
	args = append(args, inputFlags...)
	args = append(args,
		"-avoid_negative_ts", "make_zero",
		"-f", "concat",
		"-safe", "0",
		"-stream_loop", "-1",
		"-i", b.playlistFile,
		"-map", "0:v",
		"-map", "0:a?",
		"-sn",
	)

	if b.OverlayText != "" {
		encoder, presetFlags := BestHEVCEncoder()
		args = append(args, "-c:v", encoder)
		args = append(args, presetFlags...)
		args = append(args, "-crf", "23", "-tag:v", "hvc1")

		// CRITICAL: textfile is required for complex callsigns (e.g. P'''''NET:#)
		tmpFile, err := os.CreateTemp("", "gocable_callsign_*.txt")
		if err == nil {
			tmpFile.WriteString(b.OverlayText)
			tmpFile.Close()
			b.OverlayFile = tmpFile.Name()
			absPath, _ := filepath.Abs(b.OverlayFile)

			// FFmpeg filters on Windows require specific path escaping: / instead of \ and then escape the colon.
			escapedPath := strings.ReplaceAll(absPath, "\\", "/")
			escapedPath = strings.ReplaceAll(escapedPath, ":", "\\:")

			fontPath := FindSystemFont()
			fontOption := ""
			if fontPath != "" {
				escapedFontPath := strings.ReplaceAll(fontPath, "\\", "/")
				escapedFontPath = strings.ReplaceAll(escapedFontPath, ":", "\\:")
				fontOption = fmt.Sprintf("fontfile='%s':", escapedFontPath)
			}

			drawText := fmt.Sprintf("drawtext=%stextfile='%s':fontcolor=white@0.4:fontsize=24:x=w-tw-40:y=h-th-40:shadowcolor=black@0.4:shadowx=2:shadowy=2", fontOption, escapedPath)
			args = append(args, "-vf", drawText)
		} else {
			// Extreme fallback to direct text if temp file fails (using basic escaping)
			escapedText := strings.ReplaceAll(b.OverlayText, "'", "\\'")

			fontPath := FindSystemFont()
			fontOption := ""
			if fontPath != "" {
				escapedFontPath := strings.ReplaceAll(fontPath, "\\", "/")
				escapedFontPath = strings.ReplaceAll(escapedFontPath, ":", "\\:")
				fontOption = fmt.Sprintf("fontfile='%s':", escapedFontPath)
			}

			drawText := fmt.Sprintf("drawtext=%stext='%s':fontcolor=white@0.4:fontsize=24:x=w-tw-40:y=h-th-40:shadowcolor=black@0.4:shadowx=2:shadowy=2", fontOption, escapedText)
			args = append(args, "-vf", drawText)
		}
	} else {
		args = append(args, "-c:v", "copy")
	}

	if b.audioMeta == nil {
		b.audioMeta, _ = ProbeMedia(b.list.Current())
	}

	if b.audioMeta != nil && (b.audioMeta.Codec == "ac3" || b.audioMeta.Codec == "eac3") {
		// We can copy if not forcing stereo, OR if it's already stereo (or mono)
		if !b.ForceStereo || b.audioMeta.Channels <= 2 {
			args = append(args, "-c:a", "copy")
		} else {
			// ForceStereo is on and source is > 2 ch, must transcode
			args = append(args, "-c:a", "ac3", "-ac", "2", "-b:a", "192k")
			args = append(args, "-af", "aresample=async=1:min_hard_comp=1.0,loudnorm")
		}
	} else {
		channels := "6"
		bitrate := "640k"
		if b.ForceStereo || (b.audioMeta != nil && b.audioMeta.Channels > 0 && b.audioMeta.Channels <= 2) {
			channels = "2"
			bitrate = "192k"
		}
		args = append(args, "-c:a", "ac3", "-ac", channels, "-b:a", bitrate)
		args = append(args, "-af", "aresample=async=1:min_hard_comp=1.0,loudnorm")
	}

	args = append(args, outputFlags...)

	if outputURL != "-" {
		args = append(args, "-y", outputURL)
	} else {
		args = append(args, "-")
	}

	FFmpegLog.Printf("[Broadcaster] Port %d: Launching FFmpeg: ffmpeg %s\n", b.port, strings.Join(args, " "))
	b.cmd = exec.Command("ffmpeg", args...)

	stdout, err := b.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, _ := b.cmd.StderrPipe()
	LogFFmpegStderr(Log.Printf, stderr, fmt.Sprintf("FFmpeg:CH-%d", b.port))

	if err := b.cmd.Start(); err != nil {
		return err
	}

	done := make(chan struct{})
	b.relayDone = done
	go func() {
		defer close(done)
		b.relayLoop(stdout)
	}()

	overlayPath := b.OverlayFile
	go func() {
		<-done
		if b.cmd != nil {
			b.cmd.Wait() //nolint:errcheck
		}
		if overlayPath != "" {
			_ = os.Remove(overlayPath)
		}
	}()

	status := fmt.Sprintf("[Broadcaster] Port %d: READY", b.port)
	if b.OverlayText != "" {
		status += fmt.Sprintf(" | Bug: %s", b.OverlayText)
	}
	if b.audioMeta != nil && (b.audioMeta.Codec == "ac3" || b.audioMeta.Codec == "eac3") {
		if !b.ForceStereo || b.audioMeta.Channels <= 2 {
			status += " | Audio: Native"
		} else {
			status += " | Audio: Transcode (Downmix)"
		}
	} else {
		status += " | Audio: Transcode"
	}
	Log.Println(status)

	return nil
}

// acceptLoop waits for new TCP connections and spawns a sender goroutine for each.
func (b *Broadcaster) acceptLoop() {
	for {
		conn, err := b.l.Accept()
		if err != nil {
			return
		}

		b.mu.Lock()
		client := &streamClient{
			conn: conn,
			pos:  b.hub.LiveIndex(),
		}
		b.conns[conn] = client
		b.mu.Unlock()

		go b.connSender(client)
	}
}

// connSender streams data from the hub to a single TCP client.
func (b *Broadcaster) connSender(client *streamClient) {
	defer func() {
		client.conn.Close()
		b.mu.Lock()
		delete(b.conns, client.conn)
		b.mu.Unlock()
	}()

	if tcpConn, ok := client.conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetWriteBuffer(128 * 1024)
	}

	for {
		live := b.hub.LiveIndex()
		if live-client.pos > 1000 {
			client.pos = live - 20
			if client.pos < 0 {
				client.pos = 0
			}
		}

		chunk, nextPos, ok := b.hub.Get(client.pos)
		if !ok {
			return
		}

		client.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := client.conn.Write(chunk)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				client.pos = b.hub.LiveIndex() - 20
				if client.pos < 0 {
					client.pos = 0
				}
				continue
			}
			return
		}

		client.pos = nextPos
	}
}

// relayLoop reads the FFmpeg stdout and writes it to the shared hub.
func (b *Broadcaster) relayLoop(r io.Reader) {
	defer b.closeClients()
	for {
		buf := make([]byte, 188*7) // 1316 bytes (fits in 1500 MTU)
		_, err := io.ReadFull(r, buf)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				Log.Printf("[Broadcaster] Relay loop error on port %d: %v\n", b.port, err)
			}
			return
		}

		if buf[0] != 0x47 {
			continue
		}

		b.hub.Write(buf)

		if b.Protocol == "udp" && b.udpConn != nil {
			_, _ = b.udpConn.Write(buf)
		}
	}
}

// stopFFmpeg terminates the active FFmpeg process and waits for its relay to exit.
func (b *Broadcaster) stopFFmpeg() {
	b.stopMu.Lock()
	defer b.stopMu.Unlock()

	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Signal(os.Interrupt)

		// Give it a short moment to finish gracefully
		timer := time.AfterFunc(500*time.Millisecond, func() {
			if b.cmd != nil && b.cmd.Process != nil {
				_ = b.cmd.Process.Kill()
			}
		})
		defer timer.Stop()

		if b.relayDone != nil {
			<-b.relayDone
			b.relayDone = nil
		}

		// CRITICAL: Cleanup for the robust textfile callsign temporary file.
		if b.OverlayFile != "" {
			_ = os.Remove(b.OverlayFile)
			b.OverlayFile = ""
		}
		b.cmd = nil
	}
}

// closeClients forcefully disconnects all active TCP/HTTP stream clients.
func (b *Broadcaster) closeClients() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for conn := range b.conns {
		_ = conn.Close()
	}
	// Re-initialize the conns map
	b.conns = make(map[net.Conn]*streamClient)
}

// Stop terminates all streaming processes and closes all client connections.
func (b *Broadcaster) Stop() error {
	b.stopFFmpeg()
	b.closeClients()

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.hub != nil {
		b.hub.Close()
	}
	if b.l != nil {
		_ = b.l.Close()
		b.l = nil
	}
	if b.udpConn != nil {
		_ = b.udpConn.Close()
		b.udpConn = nil
	}

	if b.playlistFile != "" {
		_ = os.Remove(b.playlistFile)
		b.playlistFile = ""
	}
	return nil
}

// Repair restarts the FFmpeg process for the current media item without advancing the list.
func (b *Broadcaster) Repair() error {
	b.stopFFmpeg()
	b.closeClients()
	return b.Start()
}

// Advance skips to the next item in the media list and restarts the broadcast.
func (b *Broadcaster) Advance() error {
	b.list.Advance()
	if err := b.updatePlaylist(); err != nil {
		return err
	}
	b.stopFFmpeg()
	b.closeClients()
	return b.Start()
}

// Rewind skips back to the previous item and restarts the broadcast.
func (b *Broadcaster) Rewind() error {
	b.list.Rewind()
	if err := b.updatePlaylist(); err != nil {
		return err
	}
	b.stopFFmpeg()
	b.closeClients()
	return b.Start()
}

// StreamURL returns the formatted streaming URL for the broadcaster.
func (b *Broadcaster) StreamURL() string {
	return formatListenURL(b.Protocol, b.port)
}

// Hub returns the shared stream hub managed by this broadcaster.
func (b *Broadcaster) Hub() *StreamHub {
	return b.hub
}

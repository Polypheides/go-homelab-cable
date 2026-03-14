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
	Protocol     string // "udp", "tcp", or "http"
	cmd          *exec.Cmd
	playlistFile string
	OverlayText  string

	// TCP relay support
	mu    sync.Mutex
	conns map[net.Conn]*streamClient
	l     net.Listener
	hub   *StreamHub

	// relayDone is closed when the current relayLoop exits, used to prevent
	// overlapping relay goroutines on rapid Advance/Rewind calls.
	relayDone chan struct{}

	// stopMu guards stopFFmpeg() against concurrent calls (e.g. Stop() racing with Advance()).
	stopMu sync.Mutex

	audioMeta   *AudioMetadata
	ForceStereo bool

	// UDP relay support
	udpConn *net.UDPConn
}

type streamClient struct {
	conn net.Conn
	pos  int64 // Position in the ring buffer
}

func NewBroadcaster(list *MediaList, port int) *Broadcaster {
	return &Broadcaster{
		list:     list,
		port:     port,
		Protocol: "udp", // default
		conns:    make(map[net.Conn]*streamClient),
		hub:      NewStreamHub(16384), // 16k chunks (~4-5 seconds) safety net for high-bitrate video bursts
	}
}

func (b *Broadcaster) Init() error {
	tmpDir := os.TempDir()
	b.playlistFile = filepath.Join(tmpDir, fmt.Sprintf("cable_playlist_%d.txt", b.port))
	return b.updatePlaylist()
}

func (b *Broadcaster) updatePlaylist() error {
	var sb strings.Builder
	// FIX #2: Snapshot() holds the MediaList lock for the entire read,
	// preventing a TOCTOU race between All() and Current() calls.
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

	outputURL := "-" // ALWAYS output to stdout pipe for universal relay

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

	args := []string{
		"-re",
		"-fflags", "+genpts+igndts+discardcorrupt",
		"-analyzeduration", "5000000",
		"-probesize", "5000000",
		"-avoid_negative_ts", "make_zero",
		"-f", "concat",
		"-safe", "0",
		"-stream_loop", "-1",
		"-i", b.playlistFile,
		"-map", "0:v",
		"-map", "0:a?",
		"-sn",
	}

	// Video Encoding & Overlay
	if b.OverlayText != "" {
		encoder, presetFlags := BestHEVCEncoder()
		args = append(args, "-c:v", encoder)
		args = append(args, presetFlags...)
		args = append(args, "-crf", "23", "-tag:v", "hvc1")

		// Add the Station Bug (Callsign) in the bottom-right corner.
		// w-tw-40:h-th-40 puts it 40px from the bottom-right edges.
		// fontcolor=white@0.4 makes it semi-transparent (60% transparent).
		// We use a shadow to ensure it's readable on bright backgrounds.
		drawText := fmt.Sprintf("drawtext=text='%s':fontcolor=white@0.4:fontsize=24:x=w-tw-40:y=h-th-40:shadowcolor=black@0.4:shadowx=2:shadowy=2", b.OverlayText)
		args = append(args, "-vf", drawText)

		fmt.Printf("[Broadcaster] Port %d: Enabling %s encoding with overlay bug: %s\n", b.port, encoder, b.OverlayText)
	} else {
		// Fallback to copy if no overlay is requested to save CPU
		// But if the user wants "HEVC for everything", we'd change this to HEVC too.
		// For now, keeping copy as the high-perf default if no bug is enabled.
		args = append(args, "-c:v", "copy")
	}

	// Dynamic Audio Selection
	if b.audioMeta == nil {
		b.audioMeta, _ = ProbeMedia(b.list.Current())
	}

	if b.audioMeta != nil && (b.audioMeta.Codec == "ac3" || b.audioMeta.Codec == "eac3") && !b.ForceStereo {
		args = append(args, "-c:a", "copy")
		fmt.Printf("[Broadcaster] Port %d: Using native passthrough for %s codec\n", b.port, b.audioMeta.Codec)
	} else {
		channels := "6"
		bitrate := "640k"
		if b.ForceStereo || (b.audioMeta != nil && b.audioMeta.Channels > 0 && b.audioMeta.Channels <= 2) {
			channels = "2"
			bitrate = "192k"
		}
		args = append(args, "-c:a", "ac3", "-ac", channels, "-b:a", bitrate)
		args = append(args, "-af", "aresample=async=1:min_hard_comp=1.0,loudnorm")
		if b.audioMeta != nil {
			if b.ForceStereo && b.audioMeta.Channels > 2 {
				fmt.Printf("[Broadcaster] Port %d: Downmixing %s (%d ch) to Stereo AC3 (ForceStereo)\n", b.port, b.audioMeta.Codec, b.audioMeta.Channels)
			} else {
				fmt.Printf("[Broadcaster] Port %d: Transcoding %s (%d ch) to AC3 %s ch\n", b.port, b.audioMeta.Codec, b.audioMeta.Channels, channels)
			}
		} else {
			fmt.Printf("[Broadcaster] Port %d: Metadata probe failed, defaulting to AC3 %s ch transcoding\n", b.port, channels)
		}
	}

	args = append(args,
		"-f", "mpegts",
		"-mpegts_flags", "resend_headers+initial_discontinuity",
		"-pat_period", "0.1",
		"-y", outputURL,
	)

	b.cmd = exec.Command("ffmpeg", args...)

	stdout, err := b.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	fmt.Printf("[Broadcaster] Starting FFmpeg for port %d\n", b.port)
	if err := b.cmd.Start(); err != nil {
		return err
	}

	// Track the relay goroutine so Stop/Advance/Rewind can wait for it to exit.
	done := make(chan struct{})
	b.relayDone = done
	go func() {
		defer close(done)
		b.relayLoop(stdout)
	}()

	// Monitor FFmpeg exit in the background.
	go func() {
		<-done // Wait for relay to drain first, then reap the process.
		if b.cmd != nil {
			b.cmd.Wait() //nolint:errcheck
		}
	}()

	return nil
}

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

func (b *Broadcaster) relayLoop(r io.Reader) {
	const packetSize = 188
	const chunkPackets = 50
	chunkSize := packetSize * chunkPackets

	for {
		buf := make([]byte, chunkSize)
		_, err := io.ReadFull(r, buf)
		if err != nil {
			// io.ReadFull guarantees buf is full when err is nil, so buf[0] is safe above.
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				fmt.Printf("[Broadcaster] Relay loop error on port %d: %v\n", b.port, err)
			}
			return
		}

		// MPEG-TS sync byte check. io.ReadFull guarantees buf is exactly chunkSize bytes here.
		if buf[0] != 0x47 {
			continue
		}

		b.hub.Write(buf)

		if b.Protocol == "udp" && b.udpConn != nil {
			_, _ = b.udpConn.Write(buf)
		}
	}
}

// stopFFmpeg kills the current FFmpeg process and waits for the relay goroutine
// to finish draining. Must NOT be called while holding b.mu.
// FIX #1: stopMu prevents data races when Stop() and Advance()/Rewind() fire concurrently.
func (b *Broadcaster) stopFFmpeg() {
	b.stopMu.Lock()
	defer b.stopMu.Unlock()

	if b.cmd != nil && b.cmd.Process != nil {
		fmt.Printf("[Broadcaster] Stopping FFmpeg for port %d\n", b.port)
		_ = b.cmd.Process.Kill()
		b.cmd = nil
	}
	// Wait for the relay goroutine to exit so we don't have two relays running
	// concurrently when Start() is called again immediately after.
	if b.relayDone != nil {
		<-b.relayDone
		b.relayDone = nil
	}
}

func (b *Broadcaster) Stop() error {
	// FIX #2: stopFFmpeg (which calls cmd.Wait via relay drain) must be called
	// BEFORE acquiring b.mu, because connSender goroutines also acquire b.mu
	// in their deferred cleanup. Holding mu during Wait would deadlock them.
	b.stopFFmpeg()

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
	for conn := range b.conns {
		_ = conn.Close()
	}
	b.conns = make(map[net.Conn]*streamClient)

	// FIX #8: clean up temp playlist file on stop (covers partial Start() failures too).
	if b.playlistFile != "" {
		_ = os.Remove(b.playlistFile)
		b.playlistFile = ""
	}
	return nil
}

func (b *Broadcaster) Advance() error {
	b.list.Advance()
	if err := b.updatePlaylist(); err != nil {
		return err
	}
	b.stopFFmpeg() // safe: not holding mu
	return b.Start()
}

func (b *Broadcaster) Rewind() error {
	b.list.Rewind()
	if err := b.updatePlaylist(); err != nil {
		return err
	}
	b.stopFFmpeg() // safe: not holding mu
	return b.Start()
}

func (b *Broadcaster) StreamURL() string {
	return formatListenURL(b.Protocol, b.port)
}

func (b *Broadcaster) Hub() *StreamHub {
	return b.hub
}

var (
	hevcEncoderOnce sync.Once
	detectedEncoder string
)

// BestHEVCEncoder probes the system for the best available HEVC hardware encoder.
// It returns the encoder name and any required preset flags.
func BestHEVCEncoder() (string, []string) {
	hevcEncoderOnce.Do(func() {
		out, err := exec.Command("ffmpeg", "-encoders").Output()
		if err != nil {
			detectedEncoder = "libx265"
			return
		}

		encoders := string(out)
		priority := []string{
			"hevc_nvenc", // NVIDIA
			"hevc_qsv",   // Intel
			"hevc_amf",   // AMD
			"hevc_vaapi", // Linux / Universal
			"hevc_mf",    // Windows Media Foundation
		}

		for _, enc := range priority {
			if strings.Contains(encoders, enc) {
				detectedEncoder = enc
				return
			}
		}
		detectedEncoder = "libx265"
	})

	switch detectedEncoder {
	case "hevc_nvenc":
		return "hevc_nvenc", []string{"-preset", "p1"}
	case "hevc_qsv":
		return "hevc_qsv", []string{"-preset", "faster"}
	case "hevc_amf":
		return "hevc_amf", []string{"-quality", "speed"}
	case "hevc_vaapi":
		return "hevc_vaapi", []string{}
	case "hevc_mf":
		return "hevc_mf", []string{}
	default:
		return "libx265", []string{"-preset", "ultrafast"}
	}
}

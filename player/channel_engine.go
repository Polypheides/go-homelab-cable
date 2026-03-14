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

	// TCP relay support
	mu    sync.Mutex
	conns map[net.Conn]*streamClient
	l     net.Listener
	hub   *StreamHub

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
		"-c:v", "copy",
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
		args = append(args, "-af", "aresample=async=1:min_hard_comp=1.0")
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
	go b.relayLoop(stdout)

	fmt.Printf("[Broadcaster] Starting FFmpeg for port %d\n", b.port)
	if err := b.cmd.Start(); err != nil {
		return err
	}

	go func() {
		err := b.cmd.Wait()
		if err != nil {
			fmt.Printf("[Broadcaster] FFmpeg for port %d exited with error: %v\n", b.port, err)
		} else {
			fmt.Printf("[Broadcaster] FFmpeg for port %d exited cleanly\n", b.port)
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

		client.conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
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
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				fmt.Printf("[Broadcaster] Relay loop error on port %d: %v\n", b.port, err)
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

func (b *Broadcaster) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.stopFFmpeg()

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

	if b.playlistFile != "" {
		_ = os.Remove(b.playlistFile)
	}
	return nil
}

func (b *Broadcaster) stopFFmpeg() {
	if b.cmd != nil && b.cmd.Process != nil {
		fmt.Printf("[Broadcaster] Stopping FFmpeg for port %d\n", b.port)
		_ = b.cmd.Process.Kill()
		_ = b.cmd.Wait()
		b.cmd = nil
	}
}

func (b *Broadcaster) Advance() error {
	b.list.Advance()
	if err := b.updatePlaylist(); err != nil {
		return err
	}
	b.stopFFmpeg()
	return b.Start()
}

func (b *Broadcaster) Rewind() error {
	b.list.Rewind()
	if err := b.updatePlaylist(); err != nil {
		return err
	}
	b.stopFFmpeg()
	return b.Start()
}

func (b *Broadcaster) StreamURL() string {
	return formatListenURL(b.Protocol, b.port)
}

func (b *Broadcaster) Hub() *StreamHub {
	return b.hub
}

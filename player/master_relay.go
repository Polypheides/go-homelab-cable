package player

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)



type MasterBroadcaster struct {
	cmd       *exec.Cmd
	sourceURL string
	Protocol  string
	mu        sync.Mutex
	conns     map[any]chan []byte
	l         net.Listener
	tuneMu    sync.Mutex
	udpConn   *net.UDPConn
}

// NewMasterBroadcaster initializes a central relay engine for the active channel.
func NewMasterBroadcaster() *MasterBroadcaster {
	return &MasterBroadcaster{
		Protocol: "udp",
		conns:    make(map[any]chan []byte),
	}
}

// Tune updates the master relay to point to a new source stream URL.
func (m *MasterBroadcaster) Tune(sourceURL string) error {
	m.tuneMu.Lock()
	defer m.tuneMu.Unlock()

	m.stopFFmpeg()
	m.closeClients()

	time.Sleep(500 * time.Millisecond)
	m.sourceURL = sourceURL
	return m.start()
}

// closeClients forcefully disconnects all active relay clients.
func (m *MasterBroadcaster) closeClients() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, ch := range m.conns {
		close(ch)
		if conn, ok := key.(net.Conn); ok {
			_ = conn.Close()
		}
	}
	m.conns = make(map[any]chan []byte)
}

// start spawns the FFmpeg relay process for the master stream.
func (m *MasterBroadcaster) start() error {
	if m.sourceURL == "" {
		return nil
	}

	outputURL := "-"

	switch m.Protocol {
	case "tcp", "http":
		if m.l == nil {
			var err error
			m.l, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", MasterPort))
			if err != nil {
				return err
			}
			go m.acceptLoop()
		}
	case "udp":
		if m.udpConn == nil {
			addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", MasterPort))
			if err != nil {
				return err
			}
			m.udpConn, err = net.DialUDP("udp", nil, addr)
			if err != nil {
				return err
			}
		}
	}

	inputFlags, outputFlags := GetProtocolFlags(m.Protocol, true)

	args := []string{}
	args = append(args, inputFlags...)
	args = append(args,
		"-avoid_negative_ts", "make_zero",
		"-i", m.sourceURL,
		"-map", "0:v",
		"-map", "0:a?",
		"-sn",
		"-c", "copy",
	)
	args = append(args, outputFlags...)

	if outputURL != "-" {
		args = append(args, "-y")
	}
	args = append(args, outputURL)

	MasterLog.Printf("[Master] Starting Relay...\n")
	FFmpegLog.Printf("[Master] Launching FFmpeg Relay: ffmpeg %s\n", strings.Join(args, " "))
	m.cmd = exec.Command("ffmpeg", args...)

	stdout, err := m.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, _ := m.cmd.StderrPipe()
	LogFFmpegStderr(MasterLog.Printf, stderr, "FFmpeg:MASTER")

	if err := m.cmd.Start(); err != nil {
		return err
	}

	go m.relayLoop(stdout)

	return nil
}

// acceptLoop waits for incoming relay client connections.
func (m *MasterBroadcaster) acceptLoop() {
	for {
		conn, err := m.l.Accept()
		if err != nil {
			return
		}

		ch := make(chan []byte, 1024)
		m.mu.Lock()
		m.conns[conn] = ch
		m.mu.Unlock()

		go m.connSender(conn, ch)
	}
}

// connSender streams relay data to a single connected master client.
func (m *MasterBroadcaster) connSender(conn net.Conn, ch chan []byte) {
	defer func() {
		conn.Close()
		m.mu.Lock()
		delete(m.conns, conn)
		m.mu.Unlock()
	}()

	for buf := range ch {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := conn.Write(buf)
		if err != nil {
			return
		}
	}
}

// relayLoop reads the master FFmpeg stdout and broadcasts it to all relay clients.
func (m *MasterBroadcaster) relayLoop(r io.Reader) {
	defer m.closeClients()

	for {
		buf := make([]byte, 188*7) // 1316 bytes (fits in 1500 MTU)
		n, err := r.Read(buf)
		if n > 0 {
			packet := make([]byte, n)
			copy(packet, buf[:n])

			if m.Protocol == "udp" && m.udpConn != nil {
				_, _ = m.udpConn.Write(packet)
			}

			m.mu.Lock()
			for _, ch := range m.conns {
				select {
				case ch <- packet:
				default:
					// Skip if client buffer is full
				}
			}
			m.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// Stop terminates the master relay process and clears all client connections.
func (m *MasterBroadcaster) Stop() error {
	m.stopFFmpeg()
	if m.l != nil {
		_ = m.l.Close()
		m.l = nil
	}
	m.mu.Lock()
	for key, ch := range m.conns {
		close(ch)
		if conn, ok := key.(net.Conn); ok {
			_ = conn.Close()
		}
	}
	m.conns = make(map[any]chan []byte)
	if m.udpConn != nil {
		_ = m.udpConn.Close()
		m.udpConn = nil
	}
	m.mu.Unlock()
	return nil
}

// stopFFmpeg terminates the active master relay FFmpeg process.
func (m *MasterBroadcaster) stopFFmpeg() {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_ = m.cmd.Wait()
		m.cmd = nil
	}
}

// Stream registers a writer as a master relay client and pipes data to it.
func (m *MasterBroadcaster) Stream(ctx context.Context, w io.Writer) error {
	ch := make(chan []byte, 1024)
	// Use the channel itself as a unique key for this connection
	key := ch

	m.mu.Lock()
	m.conns[key] = ch
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.conns, key)
		m.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case buf, ok := <-ch:
			if !ok {
				return nil
			}
			_, err := w.Write(buf)
			if err != nil {
				return err
			}
		}
	}
}

// MasterStreamURL returns the fixed streaming URL for the master relay.
func MasterStreamURL(protocol string) string {
	return formatListenURL(protocol, MasterPort)
}

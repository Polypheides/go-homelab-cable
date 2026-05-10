package player

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/Polypheides/go-homelab-cable/logger"
)

var (
	Log       = logger.For("player")
	MasterLog = logger.For("master")
	FFmpegLog = logger.For("ffmpeg")
	ErrorLog  = logger.For("error")
)

const MasterPort = 4999

// formatListenURL generates a protocol-specific streaming URL for the given port.
func formatListenURL(protocol string, port int) string {
	if protocol == "tcp" || protocol == "http" {
		return fmt.Sprintf("tcp://127.0.0.1:%d", port)
	}
	return fmt.Sprintf("udp://@127.0.0.1:%d", port)
}

// GetProtocolFlags returns specialized FFmpeg flags optimized for the given protocol and stream type.
func GetProtocolFlags(protocol string, isMaster bool) (inputFlags, outputFlags []string) {
	outputFlags = []string{
		"-f", "mpegts",
		"-mpegts_flags", "resend_headers+initial_discontinuity",
		"-pat_period", "0.1",
	}

	protocol = strings.ToLower(protocol)
	if protocol == "tcp" || protocol == "http" {
		outputFlags = append(outputFlags, "-flush_packets", "1")
	}

	// Constants for probing sizes/durations
	const (
		lowProbe  = "1000000"
		medProbe  = "2000000"
		highProbe = "5000000"
		maxProbe  = "8000000"
	)

	fflags := "+genpts+igndts+discardcorrupt"
	duration, size := lowProbe, lowProbe

	switch protocol {
	case "udp":
		if !isMaster {
			duration, size = highProbe, highProbe
		} else {
			fflags += "+nobuffer"
		}
	case "tcp", "http":
		if isMaster {
			fflags += "+nobuffer"
			duration, size = medProbe, medProbe
		} else {
			duration, size = maxProbe, maxProbe
		}
	default:
		return []string{"-fflags", fflags}, outputFlags
	}

	inputFlags = []string{"-fflags", fflags, "-analyzeduration", duration, "-probesize", size}
	return inputFlags, outputFlags
}

// LogFFmpegStderr scans the FFmpeg stderr pipe and logs relevant errors to the system logger.
func LogFFmpegStderr(loggerFunc func(string, ...interface{}) (int, error), pipe io.ReadCloser, prefix string) {
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Error") || strings.Contains(line, "failed") || strings.Contains(line, "error") {
				loggerFunc("[%s] %s\n", prefix, line)
			}
		}
	}()
}

var (
	hevcEncoderOnce sync.Once
	detectedEncoder string
)

// BestHEVCEncoder identifies the optimal hardware encoder available on the host system.
func BestHEVCEncoder() (string, []string) {
	hevcEncoderOnce.Do(func() {
		out, err := exec.Command("ffmpeg", "-encoders").Output()
		if err != nil {
			detectedEncoder = "libx265"
			return
		}

		encoders := string(out)
		priority := []string{
			"hevc_nvenc",
			"hevc_qsv",
			"hevc_amf",
			"hevc_vaapi",
			"hevc_mf",
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

// FindSystemFont returns a valid absolute path to a system .ttf font for FFmpeg.
func FindSystemFont() string {
	// Standard Windows font path
	if runtime.GOOS == "windows" {
		winPath := "C:\\Windows\\Fonts\\arial.ttf"
		if _, err := os.Stat(winPath); err == nil {
			return winPath
		}
	}

	// Common Linux font paths
	linuxPaths := []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
	}

	for _, p := range linuxPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

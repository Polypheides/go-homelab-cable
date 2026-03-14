package player

import (
	"fmt"
)

const MasterPort = 4999

// formatListenURL generates a protocol-specific streaming URL for the given port.
func formatListenURL(protocol string, port int) string {
	if protocol == "tcp" || protocol == "http" {
		return fmt.Sprintf("tcp://127.0.0.1:%d", port)
	}
	return fmt.Sprintf("udp://@127.0.0.1:%d", port)
}

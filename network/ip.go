package network

import (
	"net"
	"strconv"
	"strings"
)

// GetLocalIP identifies and returns the first available non-loopback private IPv4 address.
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip := ipnet.IP.To4(); ip != nil {
				ipStr := ip.String()
				if isPrivateIP(ipStr) {
					return ipStr
				}
			}
		}
	}
	return "127.0.0.1"
}

// isPrivateIP determines if an IPv4 address falls within the RFC 1918 private ranges.
func isPrivateIP(ipStr string) bool {
	if strings.HasPrefix(ipStr, "192.168.") {
		return true
	}
	if strings.HasPrefix(ipStr, "10.") {
		return true
	}
	if strings.HasPrefix(ipStr, "172.") {
		parts := strings.SplitN(ipStr, ".", 3)
		if len(parts) >= 2 {
			second, err := strconv.Atoi(parts[1])
			if err == nil && second >= 16 && second <= 31 {
				return true
			}
		}
	}
	return false
}

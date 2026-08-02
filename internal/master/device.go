package master

import (
	"net"
	"strings"
)

// DetectLANIP picks a non-loopback IPv4 suitable for Agent advertise.
func DetectLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil {
				continue
			}
			s := ip4.String()
			if strings.HasPrefix(s, "10.") || strings.HasPrefix(s, "192.168.") || strings.HasPrefix(s, "172.") {
				return s
			}
			if fallback == "" {
				fallback = s
			}
		}
	}
	return fallback
}

// DeviceIP returns the preferred advertise IP for UI display.
func (s *Server) DeviceIP() string {
	if s.PublicHost != "" {
		host := s.PublicHost
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if host != "" && host != "0.0.0.0" && host != "::" {
			return host
		}
	}
	return DetectLANIP()
}

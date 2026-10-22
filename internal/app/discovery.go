package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	DiscoverPort = 45679
	discoverMagic = "PASTEPORT/1"
)

// DiscoverReply 是电脑对手机广播的应答。
type DiscoverReply struct {
	Magic string `json:"magic"`
	Name  string `json:"name"`
	Port  int    `json:"port"`
}

// StartDiscovery 监听局域网 UDP 广播，让 APK 不用扫码也能找到这台电脑。
// 它只回答 *谁在这里*，真正的认证仍然要走 6 位配对码。
func StartDiscovery(port int, name string, logf func(string, ...any)) (io.Closer, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: DiscoverPort})
	if err != nil {
		return nil, err
	}
	go func() {
		buf := make([]byte, 512)
		for {
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
			msg := strings.TrimSpace(string(buf[:n]))
			if !strings.HasPrefix(msg, discoverMagic) && !strings.HasPrefix(msg, "PASTEPORT?") {
				continue
			}
			reply, err := json.Marshal(DiscoverReply{Magic: discoverMagic, Name: name, Port: port})
			if err != nil {
				continue
			}
			conn.WriteToUDP(reply, from)
		}
	}()
	if logf != nil {
		logf("局域网发现已开启（UDP %d）", DiscoverPort)
	}
	return conn, nil
}

// LocalIPv4 返回本机在局域网中的地址：优先 192.168 / 10. / 172.16-31 段。
func LocalIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
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
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip[0] == 192 && ip[1] == 168 {
				return ip.String()
			}
			if ip[0] == 10 {
				return ip.String()
			}
			if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
				return ip.String()
			}
			if fallback == "" {
				fallback = ip.String()
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return "127.0.0.1"
}

func fmtURL(ip string, port int) string {
	return fmt.Sprintf("http://%s:%d/", ip, port)
}

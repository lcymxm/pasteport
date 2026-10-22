package app

import (
	"fmt"
	"net"
	"strings"
)

// virtualHints 用来识别「手机根本连不上的网卡」：
// 虚拟机、WSL、Docker、VPN、蓝牙之类。名字里带这些的一律降权。
var virtualHints = []string{
	"vethernet", "vmware", "virtualbox", "hyper-v", "loopback",
	"tap-", "tap_", "tailscale", "zerotier", "wsl", "docker", "npcap", "bluetooth",
}

// LanIPs 返回本机所有可能被手机访问到的 IPv4 地址，最可能的排在最前面。
//
// 单网卡时返回 1 个；装了虚拟机/VPN 的机器上会有多个候选，
// 这时候光看第一个很可能选错，所以全部返回，让上层把选择权交给用户。
func LanIPs() []string {
	type cand struct {
		ip    string
		score int
	}
	var found []cand

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		virtual := false
		for _, h := range virtualHints {
			if strings.Contains(name, h) {
				virtual = true
				break
			}
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
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			score := 10
			switch {
			case ip[0] == 192 && ip[1] == 168:
				score = 40 // 家用路由器最常见
			case ip[0] == 10:
				score = 35
			case ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31:
				score = 30
			}
			if iface.Flags&net.FlagBroadcast != 0 {
				score += 3 // 能广播的才像局域网口
			}
			if virtual {
				score -= 30
			}
			found = append(found, cand{ip.String(), score})
		}
	}

	// 分数高的在前；同分保持网卡顺序
	for i := 1; i < len(found); i++ {
		for j := i; j > 0 && found[j].score > found[j-1].score; j-- {
			found[j], found[j-1] = found[j-1], found[j]
		}
	}

	seen := make(map[string]bool, len(found))
	out := make([]string, 0, len(found))
	for _, c := range found {
		if seen[c.ip] || c.score < 0 {
			continue
		}
		seen[c.ip] = true
		out = append(out, c.ip)
	}
	return out
}

// PrimaryIP 返回最可能的那个地址，没有就退回回环地址。
func PrimaryIP() string {
	if ips := LanIPs(); len(ips) > 0 {
		return ips[0]
	}
	return "127.0.0.1"
}

func fmtURL(ip string, port int) string {
	return fmt.Sprintf("http://%s:%d/", ip, port)
}

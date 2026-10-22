package app

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	DiscoverPort  = 45679
	discoverMagic = "PASTEPORT/1"
)

// DiscoverReply 是电脑对手机广播的应答。
type DiscoverReply struct {
	Magic string `json:"magic"`
	Name  string `json:"name"`
	Port  int    `json:"port"`
}

// discovery 是一个可以整体关闭的发现服务。
type discovery struct {
	conn *net.UDPConn
	stop chan struct{}
	once sync.Once
}

func (d *discovery) Close() error {
	d.once.Do(func() { close(d.stop) })
	return d.conn.Close()
}

// StartDiscovery 监听局域网 UDP 广播，让 APK 不用扫码也能找到这台电脑。
//
// 设计要点（都是踩过坑换来的）：
//   - 读循环里**任何**错误都不能让协程退出。Windows 上对端一消失，
//     socket 就可能返回 WSAECONNRESET；网卡切换/睡眠唤醒也会报错。
//     早期版本在这里 return，结果就是「手机连过一次之后再也搜不到电脑」。
//   - 连续出错会重开 socket，自己恢复。
//   - 应答连发 3 次，丢一个包不至于让手机白等一轮。
func StartDiscovery(port int, name string, logf func(string, ...any)) (io.Closer, error) {
	conn, err := listenDiscovery()
	if err != nil {
		return nil, err
	}
	d := &discovery{conn: conn, stop: make(chan struct{})}
	go d.serve(port, name, logf)
	if logf != nil {
		logf("局域网发现已开启（UDP %d）", DiscoverPort)
	}
	return d, nil
}

func listenDiscovery() (*net.UDPConn, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: DiscoverPort})
	if err != nil {
		return nil, err
	}
	// Windows 默认会把 ICMP 端口不可达报成本地 socket 错误，关掉它
	disableUDPConnReset(conn)
	return conn, nil
}

func (d *discovery) serve(port int, name string, logf func(string, ...any)) {
	buf := make([]byte, 512)
	reply, _ := json.Marshal(DiscoverReply{Magic: discoverMagic, Name: name, Port: port})
	failures := 0
	var lastLog time.Time

	for {
		select {
		case <-d.stop:
			return
		default:
		}

		d.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, from, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case <-d.stop:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue // 没消息是正常的
			}

			// 其余错误一律视为「暂时性」，绝不能退出
			failures++
			if logf != nil && time.Since(lastLog) > 5*time.Second {
				logf("发现 socket 读错误（第 %d 次，已忽略）：%v", failures, err)
				lastLog = time.Now()
			}
			if failures >= 5 {
				// 连续失败说明这个 socket 废了，重开一个继续
				if fresh, e := listenDiscovery(); e == nil {
					d.conn.Close()
					d.conn = fresh
					failures = 0
					if logf != nil {
						logf("发现 socket 已重新打开")
					}
					continue
				}
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		failures = 0

		msg := strings.TrimSpace(string(buf[:n]))
		if !strings.HasPrefix(msg, discoverMagic) && !strings.HasPrefix(msg, "PASTEPORT?") {
			continue
		}

		// 连发三次，手机那边第一个包丢了也能补上
		for i := 0; i < 3; i++ {
			if _, err := d.conn.WriteToUDP(reply, from); err != nil {
				break
			}
			if i < 2 {
				time.Sleep(25 * time.Millisecond)
			}
		}

		if logf != nil && time.Since(lastLog) > time.Second {
			logf("收到发现请求：%s", from.String())
			lastLog = time.Now()
		}
	}
}

// closerFunc 让普通函数满足 io.Closer。
type closerFunc func() error

func (f closerFunc) Close() error { return f() }

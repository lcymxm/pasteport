package com.pasteport.app;

import org.json.JSONObject;

import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.SocketTimeoutException;
import java.util.ArrayList;
import java.util.List;

/**
 * 靠 UDP 广播找同一网段里的电脑。
 * 只负责「发现」，不做认证——真正的连接仍然要在网页里输入 6 位配对码。
 */
public final class Discovery {

    public static final int PORT = 45679;
    private static final String MAGIC = "PASTEPORT/1";

    public static final class Device {
        public final String name;
        public final String host;
        public final int port;

        Device(String name, String host, int port) {
            this.name = name;
            this.host = host;
            this.port = port;
        }
    }

    private Discovery() {
    }

    /** 阻塞调用，请在子线程里跑。大约 timeoutMs 毫秒后返回找到的电脑。 */
    public static List<Device> scan(int timeoutMs) {
        List<Device> found = new ArrayList<>();
        DatagramSocket sock = null;
        try {
            sock = new DatagramSocket();
            sock.setBroadcast(true);
            sock.setSoTimeout(350);

            byte[] request = (MAGIC + " DISCOVER").getBytes("UTF-8");
            DatagramPacket out = new DatagramPacket(request, request.length,
                    InetAddress.getByName("255.255.255.255"), PORT);
            sock.send(out);

            long deadline = System.currentTimeMillis() + timeoutMs;
            byte[] buf = new byte[1024];
            while (System.currentTimeMillis() < deadline) {
                try {
                    DatagramPacket in = new DatagramPacket(buf, buf.length);
                    sock.receive(in);
                    Device d = parse(new String(in.getData(), 0, in.getLength(), "UTF-8"),
                            in.getAddress().getHostAddress());
                    if (d != null && !hasHost(found, d.host)) {
                        found.add(d);
                    }
                } catch (SocketTimeoutException te) {
                    sock.send(out); // 没回音就再喊一嗓子
                }
            }
        } catch (Exception ignored) {
            // 广播被路由器挡住时只能靠手动输入地址
        } finally {
            if (sock != null) sock.close();
        }
        return found;
    }

    private static boolean hasHost(List<Device> list, String host) {
        for (Device d : list) {
            if (d.host.equals(host)) return true;
        }
        return false;
    }

    private static Device parse(String json, String fallbackHost) {
        try {
            JSONObject o = new JSONObject(json);
            if (!MAGIC.equals(o.optString("magic"))) return null;
            String name = o.optString("name", "电脑");
            int port = o.optInt("port", 8787);
            if (port <= 0 || port > 65535) return null;
            return new Device(name, fallbackHost, port);
        } catch (Exception e) {
            return null;
        }
    }
}

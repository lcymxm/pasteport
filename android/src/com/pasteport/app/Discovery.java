package com.pasteport.app;

import android.util.Log;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.HttpURLConnection;
import java.net.InetAddress;
import java.net.InterfaceAddress;
import java.net.NetworkInterface;
import java.net.Socket;
import java.net.SocketTimeoutException;
import java.net.URL;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;

/**
 * 找同一网段里的电脑。只负责「发现」，不做认证——连接仍然要在网页里输入配对码。
 *
 * 两条路，先广播后扫描：
 *   1. UDP 广播问「谁在这儿」。除了 255.255.255.255 还会往**子网定向广播**
 *      （比如 192.168.1.255）发，很多路由器和手机只在后者上放行。
 *   2. 广播被彻底挡住时，对本地 /24 网段做一次 TCP 探测，
 *      用 /api/info 确认对面确实是 PastePort。
 */
public final class Discovery {

    private static final String TAG = "PastePort";
    public static final int PORT = 45679;
    private static final String MAGIC = "PASTEPORT/1";

    /** 电脑可能监听在这些端口上（默认 8787，被占用会往后挪）。 */
    private static final int[] CANDIDATE_PORTS = {8787, 8788, 8789};

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

    /* ---------------- 广播发现 ---------------- */

    /** 阻塞调用，请在子线程里跑。 */
    public static List<Device> scan(int timeoutMs) {
        Map<String, Device> found = new LinkedHashMap<>();
        DatagramSocket sock = null;
        try {
            sock = new DatagramSocket();
            sock.setBroadcast(true);
            sock.setSoTimeout(250);

            List<InetAddress> targets = broadcastTargets();
            Log.i(TAG, "广播目标: " + targets);

            byte[] request = (MAGIC + " DISCOVER").getBytes("UTF-8");
            int next = 0;
            sendTo(sock, request, targets.get(next % targets.size()));
            next++;

            long deadline = System.currentTimeMillis() + timeoutMs;
            byte[] buf = new byte[1024];
            while (System.currentTimeMillis() < deadline) {
                try {
                    DatagramPacket in = new DatagramPacket(buf, buf.length);
                    sock.receive(in);
                    String json = new String(in.getData(), 0, in.getLength(), "UTF-8");
                    Device d = parse(json, in.getAddress().getHostAddress());
                    if (d != null) {
                        String key = d.host + ":" + d.port;
                        if (!found.containsKey(key)) {
                            Log.i(TAG, "发现电脑 " + d.name + " @ " + key);
                            found.put(key, d);
                        }
                    }
                } catch (SocketTimeoutException te) {
                    // 轮流往不同目标再喊一次：有限广播和定向广播各发几轮
                    sendTo(sock, request, targets.get(next % targets.size()));
                    next++;
                }
            }
        } catch (Exception e) {
            Log.w(TAG, "广播发现失败: " + e);
        } finally {
            if (sock != null) sock.close();
        }
        return new ArrayList<>(found.values());
    }

    private static void sendTo(DatagramSocket sock, byte[] payload, InetAddress target) {
        try {
            sock.send(new DatagramPacket(payload, payload.length, target, PORT));
        } catch (Exception ignored) {
            // 某个目标不可用就算了，别的目标还在
        }
    }

    /**
     * 有限广播 + 所有网卡上的子网定向广播。
     * 定向广播是关键：不少路由器只转发它，不转发 255.255.255.255。
     */
    private static List<InetAddress> broadcastTargets() {
        List<InetAddress> out = new ArrayList<>();
        try {
            out.add(InetAddress.getByName("255.255.255.255"));
        } catch (Exception ignored) {
        }
        try {
            for (NetworkInterface ni : Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!ni.isUp() || ni.isLoopback()) continue;
                for (InterfaceAddress ia : ni.getInterfaceAddresses()) {
                    InetAddress b = ia.getBroadcast();
                    if (b != null && !out.contains(b)) out.add(b);
                }
            }
        } catch (Exception ignored) {
        }
        return out;
    }

    /* ---------------- 网段扫描兜底 ---------------- */

    /**
     * 广播被挡住时用：对本地 /24 逐台探测常见端口，再用 /api/info 确认。
     * 只有用户主动点「重新搜索」时才会走到这里。
     *
     * @param extraPort 上次连过的端口（电脑可能不在默认端口上），0 表示没有
     */
    public static List<Device> sweepSubnet(int extraPort) {
        List<Device> found = new ArrayList<>();
        String prefix = pickPrefix();
        if (prefix == null) {
            Log.w(TAG, "找不到可用于扫描的私有网段");
            return found;
        }

        // 端口去重，上次用过的排最前面
        List<Integer> ports = new ArrayList<>();
        if (extraPort > 0) ports.add(extraPort);
        for (int p : CANDIDATE_PORTS) {
            if (!ports.contains(p)) ports.add(p);
        }
        Log.i(TAG, "开始扫描网段 " + prefix + ".0/24，端口 " + ports);

        ExecutorService pool = Executors.newFixedThreadPool(48);
        for (int host = 1; host <= 254; host++) {
            final String ip = prefix + "." + host;
            pool.submit(new Runnable() {
                @Override
                public void run() {
                    for (int port : ports) {
                        if (probe(ip, port)) {
                            synchronized (found) {
                                found.add(new Device("电脑", ip, port));
                            }
                            return;
                        }
                    }
                }
            });
        }
        pool.shutdown();
        try {
            pool.awaitTermination(15, TimeUnit.SECONDS);
        } catch (InterruptedException ignored) {
            Thread.currentThread().interrupt();
        }

        // 用 /api/info 里的名字补齐（只对命中的少数几台发请求）
        List<Device> named = new ArrayList<>();
        synchronized (found) {
            for (Device d : found) {
                String name = fetchName(d.host, d.port);
                named.add(new Device(name != null ? name : d.name, d.host, d.port));
            }
        }
        Log.i(TAG, "网段扫描结束，命中 " + named.size() + " 台");
        return named;
    }

    /**
     * 从私有地址网卡里取出前三段。优先 Wi-Fi，避免把 VPN / 蜂窝网络的网段当成本地网段。
     * 不要求掩码是 /24 —— 公司网常见 /16 甚至 /8，扫它所在的 /24 就够用了。
     */
    private static String pickPrefix() {
        String wifi = null;
        String fallback = null;
        try {
            for (NetworkInterface ni : Collections.list(NetworkInterface.getNetworkInterfaces())) {
                if (!ni.isUp() || ni.isLoopback()) continue;
                String niName = ni.getName() == null ? "" : ni.getName().toLowerCase();
                for (InterfaceAddress ia : ni.getInterfaceAddresses()) {
                    InetAddress ip = ia.getAddress();
                    if (ip == null) continue;
                    byte[] raw = ip.getAddress();
                    if (raw.length != 4 || !isPrivate(raw)) continue;
                    String p = (raw[0] & 0xFF) + "." + (raw[1] & 0xFF) + "." + (raw[2] & 0xFF);
                    boolean looksWifi = niName.startsWith("wlan") || niName.contains("wifi")
                            || niName.startsWith("ap") || niName.contains("rmnet");
                    if (looksWifi) {
                        wifi = p;
                    } else if (fallback == null) {
                        fallback = p;
                    }
                }
            }
        } catch (Exception e) {
            Log.w(TAG, "枚举网卡失败: " + e);
        }
        return wifi != null ? wifi : fallback;
    }

    /** 10/8、172.16/12、192.168/16 才算局域网。 */
    private static boolean isPrivate(byte[] a) {
        int b0 = a[0] & 0xFF, b1 = a[1] & 0xFF;
        if (b0 == 10) return true;
        if (b0 == 192 && b1 == 168) return true;
        return b0 == 172 && b1 >= 16 && b1 <= 31;
    }

    /** 端口能不能连上。 */
    private static boolean probe(String host, int port) {
        Socket s = new Socket();
        try {
            s.connect(new java.net.InetSocketAddress(host, port), 250);
            return true;
        } catch (Exception e) {
            return false;
        } finally {
            try { s.close(); } catch (Exception ignored) { }
        }
    }

    /** 连上之后再确认一次「对面是不是 PastePort」，避免把别的服务当电脑。 */
    private static String fetchName(String host, int port) {
        HttpURLConnection c = null;
        try {
            URL url = new URL("http://" + host + ":" + port + "/api/info");
            c = (HttpURLConnection) url.openConnection();
            c.setConnectTimeout(800);
            c.setReadTimeout(800);
            if (c.getResponseCode() != 200) return null;
            StringBuilder sb = new StringBuilder();
            BufferedReader r = new BufferedReader(new InputStreamReader(c.getInputStream(), "UTF-8"));
            String line;
            while ((line = r.readLine()) != null) sb.append(line);
            r.close();
            JSONObject o = new JSONObject(sb.toString());
            if (!o.optBoolean("pair", false)) return null;   // 不是 PastePort
            return o.optString("name", "电脑");
        } catch (Exception e) {
            return null;
        } finally {
            if (c != null) c.disconnect();
        }
    }

    /* ---------------- 解析 ---------------- */

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

"""PastePort 服务端黑盒测试。

    python api_test.py <port>

自动起一个 SSE 连接，然后依次打接口，报告不符合预期的点。
"""
import http.client
import json
import socket
import sys
import threading
import time

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 8787

fails = []
oks = 0


def check(name, cond, detail=""):
    global oks
    if cond:
        oks += 1
        print(f"  \u2713 {name}")
    else:
        fails.append(f"{name}  {detail}")
        print(f"  \u2717 {name}  {detail}")


def req(method, path, body=None, token=None, timeout=6, raw=None, ctype=None):
    c = http.client.HTTPConnection("127.0.0.1", PORT, timeout=timeout)
    headers = {}
    if token:
        headers["X-PP-Token"] = token
    if raw is not None:
        data = raw
        if ctype:
            headers["Content-Type"] = ctype
    elif body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    else:
        data = None
    c.request(method, path, body=data, headers=headers)
    r = c.getresponse()
    payload = r.read()
    c.close()
    return r.status, payload


# ---------- SSE 收集器 ----------
events = []
stop = threading.Event()


def sse_worker(token):
    s = socket.create_connection(("127.0.0.1", PORT), timeout=8)
    s.sendall(
        f"GET /api/events?t={token} HTTP/1.1\r\nHost: x\r\nAccept: text/event-stream\r\n\r\n".encode()
    )
    buf = b""
    s.settimeout(0.5)
    while not stop.is_set():
        try:
            chunk = s.recv(4096)
            if not chunk:
                break
            buf += chunk
        except socket.timeout:
            continue
        except OSError:
            break
        while b"\n\n" in buf:
            block, buf = buf.split(b"\n\n", 1)
            for line in block.split(b"\n"):
                if line.startswith(b"data: "):
                    try:
                        events.append(json.loads(line[6:]))
                    except Exception:
                        pass
    s.close()


def main():
    print(f"\n目标 http://127.0.0.1:{PORT}")

    print("\n[1] 认证")
    st, _ = req("GET", "/api/state")
    check("无 token 访问 /api/state 被拒", st == 401, f"实际 {st}")
    st, b = req("POST", "/api/local")
    check("/api/local 本机可取 token", st == 200, f"实际 {st}")
    tok = json.loads(b)["token"]
    st, b = req("GET", "/api/state", token=tok)
    check("带 token 可取状态", st == 200, f"实际 {st}")
    state = json.loads(b)
    check("版本号是 0.3.x", state["version"].startswith("0.3"), state.get("version"))

    st, _ = req("GET", "/api/state", token="0" * len(tok))
    check("错误 token 被拒", st == 401, f"实际 {st}")

    print("\n[2] SSE 与回声抑制")
    t = threading.Thread(target=sse_worker, args=(tok,), daemon=True)
    t.start()
    time.sleep(0.6)
    check("连上后收到 hello", any(e.get("type") == "hello" for e in events), str(events[:2]))

    uniq = "回声测试 \U0001F600 line1\nline2\t制表"
    st, b = req("POST", "/api/clip", {"text": uniq, "from": "t1", "name": "测试"}, token=tok)
    check("推送剪贴板成功", st == 200, f"实际 {st} {b[:80]}")
    time.sleep(2.0)

    clips = [e for e in events if e.get("type") == "clip"]
    check("只收到 1 条 clip 事件（没有回声）", len(clips) == 1, f"收到 {len(clips)} 条")
    if clips:
        check("内容原样到达（含 emoji/换行）", clips[0]["text"] == uniq,
              repr(clips[0]["text"])[:60])

    print("\n[3] 文件上传 / 下载")
    boundary = "----PPTEST"
    evil = "../../evil.txt"
    body = (
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"from\"\r\n\r\nt1\r\n"
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{evil}\"\r\n"
        f"Content-Type: text/plain\r\n\r\nhello-file\r\n--{boundary}--\r\n"
    ).encode()
    st, b = req("POST", "/api/upload", raw=body, ctype=f"multipart/form-data; boundary={boundary}", token=tok)
    check("上传成功", st == 200, f"实际 {st} {b[:120]}")
    files = json.loads(b).get("files", []) if st == 200 else []
    if files:
        name = files[0]["name"]
        check("文件名里的 ../ 被清洗", ".." not in name and "/" not in name and "\\" not in name, name)
        fid = files[0]["id"]
        st, data = req("GET", f"/api/download?id={fid}", token=tok)
        check("下载拿回原内容", st == 200 and data == b"hello-file", f"{st} {data[:20]!r}")
    st, _ = req("GET", "/api/download?id=deadbeef", token=tok)
    check("不存在的 id 返回 404", st == 404, f"实际 {st}")

    print("\n[4] 配对锁定")
    for i in range(6):
        st, b = req("POST", "/api/pair", {"code": "000000"})
        if i == 5:
            check("连错 6 次后返回 429", st == 429, f"第 6 次实际 {st} {b[:80]}")

    print("\n[5] 边界")
    st, b = req("GET", "/api/qr?i=0", token=tok)
    check("/api/qr 返回 PNG", st == 200 and b[:8] == b"\x89PNG\r\n\x1a\n", f"{st} {b[:12]!r}")

    st, b = req("POST", "/api/clip", {"text": "x" * (2 * 1024 * 1024), "from": "t"}, token=tok)
    check("2MB 文本被拒绝而不是撑爆", st in (400, 413), f"实际 {st}")

    st, b = req("POST", "/api/clip", {"text": "", "from": "t"}, token=tok)
    check("空文本返回 4xx", st in (400, 413), f"实际 {st}")

    st, b = req("GET", "/api/info")
    check("/api/info 免认证可用", st == 200, f"实际 {st}")

    stop.set()
    print(f"\n通过 {oks}，失败 {len(fails)}")
    for f in fails:
        print("  FAIL:", f)
    return 0 if not fails else 1


if __name__ == "__main__":
    sys.exit(main())

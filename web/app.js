/* PastePort 前端 —— 无框架、无构建步骤，一个文件搞定。
   运行环境既可能是电脑浏览器，也可能是手机浏览器/WebView。 */
(function () {
  'use strict';

  var $ = function (sel, root) { return (root || document).querySelector(sel); };

  var isPhone = /Android|iPhone|iPad|iPod|Mobile/i.test(navigator.userAgent);
  if (isPhone) document.body.classList.add('phone');

  var S = {
    token: localStorage.getItem('pp.token') || '',
    cid: 'c' + Math.random().toString(36).slice(2, 8),
    name: isPhone ? '手机' : '电脑',
    url: '',
    urls: [],
    es: null,
    seen: Object.create(null),
    got: 0
  };

  /* ---------------- 工具 ---------------- */

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  function toast(msg, bad) {
    var el = $('#toast');
    el.textContent = msg;
    el.className = 'toast show' + (bad ? ' bad' : '');
    clearTimeout(toast._t);
    toast._t = setTimeout(function () { el.className = 'toast'; }, 2200);
  }

  function humanSize(n) {
    n = Number(n) || 0;
    if (n < 1024) return n + ' B';
    var u = ['KB', 'MB', 'GB', 'TB'], i = -1;
    do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
    return n.toFixed(n < 10 ? 1 : 0) + ' ' + u[i];
  }

  function hhmm(iso) {
    var d = iso ? new Date(iso) : new Date();
    if (isNaN(d.getTime())) d = new Date();
    return ('0' + d.getHours()).slice(-2) + ':' + ('0' + d.getMinutes()).slice(-2);
  }

  function spaced(code) {
    return code && code.length === 6 ? code.slice(0, 3) + ' ' + code.slice(3) : (code || '—');
  }

  function setStatus(kind, text) {
    var el = $('#status');
    el.className = 'status' + (kind ? ' ' + kind : '');
    $('#statusText').textContent = text;
  }

  /* ---------------- 剪贴板 ---------------- */

  async function copy(text) {
    if (window.PastePort && typeof window.PastePort.copy === 'function') {
      try { window.PastePort.copy(text); return true; } catch (e) { /* 继续尝试 */ }
    }
    if (navigator.clipboard && window.isSecureContext) {
      try { await navigator.clipboard.writeText(text); return true; } catch (e) { /* 继续尝试 */ }
    }
    try {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      ta.style.cssText = 'position:fixed;top:-1000px;left:-1000px;opacity:0';
      document.body.appendChild(ta);
      ta.select();
      ta.setSelectionRange(0, text.length);
      var ok = document.execCommand('copy');
      document.body.removeChild(ta);
      return ok;
    } catch (e) {
      return false;
    }
  }

  /* ---------------- 网络 ---------------- */

  async function api(path, opts) {
    opts = opts || {};
    opts.headers = Object.assign({ 'X-PP-Token': S.token }, opts.headers || {});
    var res = await fetch(path, opts);
    if (res.status === 401) {
      S.token = '';
      localStorage.removeItem('pp.token');
      throw new Error('unauthorized');
    }
    var data = null;
    try { data = await res.json(); } catch (e) { data = null; }
    if (!res.ok) throw new Error((data && data.error) || ('HTTP ' + res.status));
    return data;
  }

  /* ---------------- 配对 ---------------- */

  function showPair(msg) {
    $('#mainView').classList.add('hidden');
    $('#pairView').classList.remove('hidden');
    setStatus('', '未连接');
    if (msg) $('#pairErr').textContent = msg;
    var input = $('#code');
    input.value = '';
    setTimeout(function () { input.focus(); }, 60);
  }

  async function doPair(code) {
    code = (code || '').replace(/\D/g, '');
    if (code.length !== 6) { $('#pairErr').textContent = '请输入 6 位数字'; return; }
    $('#btnPair').disabled = true;
    $('#pairErr').textContent = '';
    try {
      var res = await fetch('/api/pair', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: code })
      });
      var data = await res.json();
      if (!res.ok) throw new Error(data.error || '配对失败');
      S.token = data.token;
      localStorage.setItem('pp.token', S.token);
      history.replaceState(null, '', location.pathname);
      await afterAuth();
      toast('已连接：' + (data.name || '电脑'));
    } catch (e) {
      $('#pairErr').textContent = e.message || '配对失败';
    } finally {
      $('#btnPair').disabled = false;
    }
  }

  /* ---------------- 主流程 ---------------- */

  async function afterAuth() {
    var st;
    try {
      st = await api('/api/state');
    } catch (e) {
      if (e.message === 'unauthorized') showPair('连接已失效，请重新配对');
      else showPair('连不上电脑：' + e.message);
      return;
    }
    S.url = st.url || '';
    S.urls = st.urls && st.urls.length ? st.urls : [S.url];
    $('#pairView').classList.add('hidden');
    $('#mainView').classList.remove('hidden');
    $('#ver').textContent = st.version || '';
    $('#codeBig').textContent = spaced(st.code);

    var sel = $('#ipSelect');
    if (S.urls.length > 1) {
      sel.innerHTML = S.urls.map(function (u, i) {
        return '<option value="' + i + '">' + esc(u) + '</option>';
      }).join('');
      sel.classList.remove('hidden');
      sel.value = '0';
      $('#deviceLine').textContent = '有多个网卡，扫不出来就换一个地址试试';
    } else {
      sel.classList.add('hidden');
      $('#deviceLine').textContent = '保存到 ' + (st.save_dir || '');
    }
    applyQR(0);
    $('#hint').textContent = isPhone ? '点右侧发送' : 'Ctrl + Enter';
    $('#startup').checked = !!st.startup;

    // 告诉 APK 记住地址和令牌：分享菜单和下次启动都要用（浏览器里没有这个对象）
    if (window.PastePort && typeof window.PastePort.remember === 'function') {
      try { window.PastePort.remember(S.token, location.origin); } catch (e) { /* 忽略 */ }
    }

    if (st.files) {
      st.files.forEach(function (f) { addFile(f, true); });
    }
    if (st.history) {
      st.history.slice().reverse().forEach(function (ev) {
        if (ev.type === 'clip') addClip(ev);
        else if (ev.type === 'image' && ev.file) addImage(ev.file);
        else if (ev.type === 'file' && ev.file) addFile(ev.file, true);
      });
    }
    updateEmpties();
    connect(st.devices || 0);
  }

  function connect(devices) {
    if (S.es) { S.es.close(); S.es = null; }
    var es = new EventSource('/api/events?t=' + encodeURIComponent(S.token) + '&n=' + encodeURIComponent(S.name));
    S.es = es;

    es.onopen = function () { setStatus('on', '已连接'); };

    es.onmessage = function (e) {
      var ev;
      try { ev = JSON.parse(e.data); } catch (err) { return; }
      if (ev.type === 'hello') { setStatus('on', '已连接'); return; }
      handle(ev);
    };

    es.onerror = function () {
      setStatus('err', '重连中…');
    };
  }

  function handle(ev) {
    if (ev.id && S.seen[ev.id]) return;
    if (ev.id) S.seen[ev.id] = 1;

    if (ev.type === 'clip') {
      addClip(ev);
      if (isPhone && ev.from === 'pc') autoCopy(ev.text);
    } else if (ev.type === 'image' && ev.file) {
      addImage(ev.file);
      toast(isPhone ? '收到一张截图' : '收到图片');
    } else if (ev.type === 'file' && ev.file) {
      addFile(ev.file, false);
      toast('收到文件：' + ev.file.name);
    }
    updateEmpties();
  }

  async function autoCopy(text) {
    if (!text) return;
    var ok = await copy(text);
    toast(ok ? '已复制到剪贴板' : '收到新内容，点「复制」取用', !ok);
  }

  /* ---------------- 渲染 ---------------- */

  // 换一个网卡地址：二维码和显示的文字一起切
  function applyQR(idx) {
    if (!S.urls || !S.urls.length) { S.urls = [S.url || location.origin]; }
    if (!(idx >= 0 && idx < S.urls.length)) { idx = 0; }
    $('#qr').src = '/api/qr?t=' + encodeURIComponent(S.token) + '&i=' + idx;
    $('#urlText').textContent = S.urls[idx];
  }

  function addClip(ev) {
    var feed = $('#feed');
    if (ev.id && feed.querySelector('[data-id="' + ev.id + '"]')) return;
    var mine = ev.from === S.cid;
    var who = mine ? '我' : (ev.from === 'pc' ? '电脑' : (ev.from_name || '手机'));
    var li = document.createElement('li');
    li.className = 'item' + (ev.from === 'pc' ? '' : ' from-phone');
    if (ev.id) li.dataset.id = ev.id;
    var long = (ev.text || '').length > 220;
    li.innerHTML =
      '<div class="meta"><b>' + esc(who) + '</b><span>' + hhmm(ev.at) + '</span>' +
      (long ? '<span>· ' + (ev.text || '').length + ' 字</span>' : '') + '</div>' +
      '<p class="text">' + esc(ev.text) + '</p>' +
      '<div class="row-end"><button class="mini act-copy">复制</button></div>';
    li.querySelector('.act-copy').addEventListener('click', async function () {
      var ok = await copy(ev.text);
      toast(ok ? '已复制' : '复制失败，请长按选中文字', !ok);
    });
    feed.prepend(li);
    while (feed.children.length > 40) feed.removeChild(feed.lastElementChild);
  }

  function addImage(f) {
    var feed = $('#feed');
    if (f.id && feed.querySelector('[data-id="' + f.id + '"]')) return;
    var href = '/api/download?id=' + encodeURIComponent(f.id) + '&t=' + encodeURIComponent(S.token);
    var li = document.createElement('li');
    li.className = 'item';
    if (f.id) li.dataset.id = f.id;
    li.innerHTML =
      '<div class="meta"><b>电脑截图</b><span>' + hhmm(f.at) + '</span>' +
      '<span>· ' + humanSize(f.size) + '</span></div>' +
      '<a class="shot" href="' + href + '" target="_blank" rel="noreferrer">' +
      '<img src="' + href + '" alt="截图" loading="lazy"></a>' +
      '<div class="row-end">' +
      '<button class="mini act-copy-img">复制</button>' +
      '<a class="mini" download="' + esc(f.name) + '" href="' + href + '">保存</a>' +
      '</div>';
    li.querySelector('.act-copy-img').addEventListener('click', async function () {
      try {
        var res = await fetch(href);
        var blob = await res.blob();
        if (navigator.clipboard && window.ClipboardItem && window.isSecureContext) {
          await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]);
          toast('图片已复制');
        } else {
          toast('这个浏览器不支持复制图片，用「保存」吧', true);
        }
      } catch (e) {
        toast('复制失败，用「保存」吧', true);
      }
    });
    feed.prepend(li);
    while (feed.children.length > 40) feed.removeChild(feed.lastElementChild);
  }

  function addFile(f, quiet) {
    var list = $('#files');
    if (f.id && list.querySelector('[data-id="' + f.id + '"]')) return;
    var li = document.createElement('li');
    li.className = 'file-item';
    if (f.id) li.dataset.id = f.id;
    li.innerHTML =
      '<div class="file-icon"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
      'stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/></svg></div>' +
      '<div class="file-body"><span class="file-name">' + esc(f.name) + '</span>' +
      '<span class="file-meta">' + humanSize(f.size) + ' · ' + hhmm(f.at) + '</span></div>' +
      '<a class="mini" download href="/api/download?id=' + encodeURIComponent(f.id) +
      '&t=' + encodeURIComponent(S.token) + '">下载</a>';
    list.prepend(li);
    while (list.children.length > 30) list.removeChild(list.lastElementChild);
  }

  function updateEmpties() {
    $('#feedEmpty').classList.toggle('hidden', $('#feed').children.length > 0);
    $('#filesEmpty').classList.toggle('hidden', $('#files').children.length > 0);
  }

  /* ---------------- 发送 ---------------- */

  async function sendText() {
    var ta = $('#text');
    var text = ta.value;
    if (!text.trim()) { ta.focus(); return; }
    var btn = $('#btnSend');
    btn.disabled = true;
    try {
      await api('/api/clip', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: text, from: S.cid, name: S.name })
      });
      ta.value = '';
      toast('已发送');
    } catch (e) {
      toast(e.message === 'unauthorized' ? '连接已失效，请重新配对' : '发送失败：' + e.message, true);
    } finally {
      btn.disabled = false;
    }
  }

  async function sendFiles(files) {
    if (!files || !files.length) return;
    var fd = new FormData();
    fd.append('from', S.cid);
    fd.append('name', S.name);
    for (var i = 0; i < files.length; i++) fd.append('file', files[i], files[i].name);
    toast('正在发送 ' + files.length + ' 个文件…');
    try {
      await api('/api/upload', { method: 'POST', body: fd });
      toast('已发送');
    } catch (e) {
      toast('发送失败：' + e.message, true);
    }
  }

  /* ---------------- 事件绑定 ---------------- */

  $('#btnPair').addEventListener('click', function () { doPair($('#code').value); });
  $('#code').addEventListener('keydown', function (e) {
    if (e.key === 'Enter') doPair($('#code').value);
  });
  $('#code').addEventListener('input', function () {
    this.value = this.value.replace(/\D/g, '').slice(0, 6);
  });

  $('#btnSend').addEventListener('click', sendText);
  $('#text').addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); sendText(); }
  });
  $('#file').addEventListener('change', function () {
    sendFiles(this.files);
    this.value = '';
  });
  $('#btnCopyUrl').addEventListener('click', async function () {
    var ok = await copy($('#urlText').textContent);
    toast(ok ? '地址已复制' : '复制失败', !ok);
  });
  $('#btnClear').addEventListener('click', function () {
    $('#feed').innerHTML = '';
    updateEmpties();
  });

  $('#ipSelect').addEventListener('change', function () {
    applyQR(parseInt(this.value, 10) || 0);
  });

  $('#startup').addEventListener('change', async function () {
    var box = this;
    var want = box.checked;    try {
      var r = await api('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ startup: want })
      });
      box.checked = !!r.startup;
      toast(r.startup ? '已加入开机自启' : '已取消开机自启');
    } catch (e) {
      box.checked = !want;
      toast(e.message === 'unauthorized' ? '连接已失效' : '设置失败：' + e.message, true);
    }
  });

  // 拖拽发送（桌面端）
  var dragDepth = 0;
  window.addEventListener('dragenter', function (e) {
    if (!e.dataTransfer || Array.prototype.indexOf.call(e.dataTransfer.types, 'Files') < 0) return;
    dragDepth++; $('#dropzone').classList.add('on');
  });
  window.addEventListener('dragover', function (e) { e.preventDefault(); });
  window.addEventListener('dragleave', function () {
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) $('#dropzone').classList.remove('on');
  });
  window.addEventListener('drop', function (e) {
    e.preventDefault();
    dragDepth = 0; $('#dropzone').classList.remove('on');
    if (e.dataTransfer && e.dataTransfer.files.length) sendFiles(e.dataTransfer.files);
  });

  // 切回前台时如果连接断了，重连
  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState === 'visible' && S.token && (!S.es || S.es.readyState === 2)) {
      afterAuth();
    }
  });

  /* ---------------- 启动 ---------------- */

  (async function boot() {
    var m = /#c=(\d{6})/.exec(location.hash);
    if (m) { await doPair(m[1]); return; }

    if (S.token) {
      try {
        await api('/api/state');
        await afterAuth();
        return;
      } catch (e) {
        S.token = '';
        localStorage.removeItem('pp.token');
      }
    }

    // 只有在电脑本机（回环地址）访问时才自动拿令牌
    if (!isPhone) {
      try {
        var res = await fetch('/api/local', { method: 'POST' });
        if (res.ok) {
          var d = await res.json();
          S.token = d.token;
          localStorage.setItem('pp.token', S.token);
          await afterAuth();
          return;
        }
      } catch (e) { /* 忽略，走配对流程 */ }
    }

    showPair('');
  })();
})();

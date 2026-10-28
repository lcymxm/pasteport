package com.pasteport.app;

import android.app.Activity;
import android.content.Context;
import android.content.Intent;
import android.graphics.Typeface;
import android.graphics.drawable.ColorDrawable;
import android.graphics.drawable.GradientDrawable;
import android.net.wifi.WifiManager;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import java.util.List;

/**
 * PastePort 安卓端。
 *
 * 它本身不实现同步逻辑，界面就是电脑上跑的那套网页（局域网内直接访问）。
 * 原生这层只做四件网页做不到的事：
 *   1. UDP 广播自动发现电脑（不用手输 IP）
 *   2. 真正的剪贴板读写（http 页面拿不到 navigator.clipboard）
 *   3. 记住地址和令牌，下次直接进
 *   4. 从别的 App「分享 → PastePort」直接发到电脑
 */
public class MainActivity extends Activity {

    private static final int C_BG = 0xFF0A0C0F;
    private static final int C_CARD = 0xFF131920;
    private static final int C_LINE = 0xFF1E2833;
    private static final int C_FG = 0xFFE8EEF4;
    private static final int C_DIM = 0xFF9AA8B6;
    private static final int C_ACC = 0xFF2DD4BF;

    private final Handler ui = new Handler(Looper.getMainLooper());

    private WebView web;
    private LinearLayout deviceList;
    private ProgressBar spinner;
    private TextView hint;
    private boolean scanning;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().setBackgroundDrawable(new ColorDrawable(C_BG));
        getWindow().setStatusBarColor(C_BG);
        getWindow().setNavigationBarColor(C_BG);

        if (handleShare(getIntent())) return;

        String url = Prefs.url(this);
        if (url != null) {
            openWeb(url);
        } else {
            showDiscover();
        }
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        handleShare(intent);
    }

    /* ---------------- 分享进入 ---------------- */

    private boolean handleShare(Intent intent) {
        if (intent == null || !Intent.ACTION_SEND.equals(intent.getAction())) return false;
        if (!"text/plain".equals(intent.getType())) return false;
        CharSequence cs = intent.getCharSequenceExtra(Intent.EXTRA_TEXT);
        if (cs == null || cs.toString().trim().isEmpty()) return false;

        final String text = cs.toString();
        final String base = Prefs.url(this);
        final String token = Prefs.token(this);
        if (base == null || token == null) {
            toast("请先连接一次电脑");
            return false;
        }
        toast("正在发送到电脑…");
        new Thread(new Runnable() {
            @Override
            public void run() {
                final boolean ok = Net.postClip(base, token, text);
                ui.post(new Runnable() {
                    @Override
                    public void run() {
                        toast(ok ? "已发送到电脑剪贴板" : "发送失败，确认电脑还开着且在同一 Wi-Fi");
                        finish();
                    }
                });
            }
        }).start();
        return true;
    }

    /* ---------------- 发现页 ---------------- */

    private void showDiscover() {
        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.setBackgroundColor(C_BG);

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(22), dp(36), dp(22), dp(28));
        scroll.addView(root, new ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        TextView title = new TextView(this);
        title.setText("PastePort");
        title.setTextColor(C_FG);
        title.setTextSize(TypedValue.COMPLEX_UNIT_SP, 27);
        title.setTypeface(Typeface.create("sans-serif", Typeface.BOLD));
        root.addView(title);

        TextView sub = new TextView(this);
        sub.setText("连接同一 Wi-Fi 下的电脑，之后复制粘贴直接互通");
        sub.setTextColor(C_DIM);
        sub.setTextSize(TypedValue.COMPLEX_UNIT_SP, 14);
        sub.setPadding(0, dp(6), 0, dp(22));
        root.addView(sub);

        spinner = new ProgressBar(this);
        LinearLayout.LayoutParams sp = new LinearLayout.LayoutParams(dp(26), dp(26));
        sp.bottomMargin = dp(12);
        spinner.setLayoutParams(sp);
        root.addView(spinner);

        hint = new TextView(this);
        hint.setText("正在搜索局域网…");
        hint.setTextColor(C_DIM);
        hint.setTextSize(TypedValue.COMPLEX_UNIT_SP, 14);
        hint.setPadding(0, 0, 0, dp(16));
        root.addView(hint);

        deviceList = new LinearLayout(this);
        deviceList.setOrientation(LinearLayout.VERTICAL);
        root.addView(deviceList);

        Button rescan = makeButton("重新搜索", false);
        rescan.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                scan();
            }
        });
        LinearLayout.LayoutParams rp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, dp(46));
        rp.topMargin = dp(10);
        root.addView(rescan, rp);

        // 手动输入：有些路由器会拦掉 UDP 广播
        TextView or = new TextView(this);
        or.setText("找不到？手动输入电脑上显示的地址");
        or.setTextColor(C_DIM);
        or.setTextSize(TypedValue.COMPLEX_UNIT_SP, 12.5f);
        or.setPadding(0, dp(28), 0, dp(10));
        root.addView(or);

        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);

        final EditText input = new EditText(this);
        input.setHint("192.168.1.5:8787");
        input.setHintTextColor(0xFF4B5764);
        input.setTextColor(C_FG);
        input.setTextSize(TypedValue.COMPLEX_UNIT_SP, 14);
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        input.setSingleLine(true);
        input.setBackground(makeCard(12, C_BG, C_LINE));
        input.setPadding(dp(12), dp(10), dp(12), dp(10));
        LinearLayout.LayoutParams ilp = new LinearLayout.LayoutParams(0, dp(46), 1f);
        ilp.rightMargin = dp(8);
        row.addView(input, ilp);

        Button go = makeButton("连接", true);
        go.setOnClickListener(new View.OnClickListener() {
            @Override
            public void onClick(View v) {
                String raw = input.getText().toString().trim();
                if (raw.isEmpty()) {
                    toast("先填电脑地址");
                    return;
                }
                openWeb(normalize(raw));
            }
        });
        row.addView(go, new LinearLayout.LayoutParams(dp(88), dp(46)));
        root.addView(row);

        TextView foot = new TextView(this);
        foot.setText("只申请网络权限 · 数据只在局域网内直连 · 不经过任何服务器");
        foot.setTextColor(0xFF4B5764);
        foot.setTextSize(TypedValue.COMPLEX_UNIT_SP, 11.5f);
        foot.setPadding(0, dp(34), 0, 0);
        foot.setGravity(Gravity.CENTER);
        root.addView(foot);

        setContentView(scroll);
        scan();
    }

    private void scan() {
        if (scanning) return;
        scanning = true;
        deviceList.removeAllViews();
        spinner.setVisibility(View.VISIBLE);
        hint.setText("正在搜索局域网…");

        new Thread(new Runnable() {
            @Override
            public void run() {
                // 有些机器的 Wi-Fi 芯片默认丢弃组播/广播，拿个锁再扫
                WifiManager.MulticastLock lock = null;
                try {
                    WifiManager wm = (WifiManager) getApplicationContext()
                            .getSystemService(Context.WIFI_SERVICE);
                    if (wm != null) {
                        lock = wm.createMulticastLock("pasteport-discovery");
                        lock.setReferenceCounted(false);
                        lock.acquire();
                    }
                } catch (Exception ignored) {
                    // 拿不到锁也继续，广播多半还是能收到的
                }

                List<Discovery.Device> found;
                try {
                    found = Discovery.scan(4000);
                    // 广播被路由器挡住时，退回网段扫描（用户点按钮就是在明确要求「找找看」）
                    if (found.isEmpty()) {
                        ui.post(new Runnable() {
                            @Override
                            public void run() {
                                hint.setText("广播没回应，正在扫描本网段…");
                            }
                        });
                        found = Discovery.sweepSubnet(lastPort());
                    }
                } finally {
                    if (lock != null) {
                        try { lock.release(); } catch (Exception ignored) { }
                    }
                }

                final List<Discovery.Device> result = found;
                ui.post(new Runnable() {
                    @Override
                    public void run() {
                        scanning = false;
                        spinner.setVisibility(View.GONE);
                        renderDevices(result);
                    }
                });
            }
        }).start();
    }

    private void renderDevices(List<Discovery.Device> found) {
        deviceList.removeAllViews();
        if (found.isEmpty()) {
            hint.setText("没找到电脑。确认电脑上的 PastePort 正在运行、且手机和电脑连的是同一个 Wi-Fi。");
            return;
        }
        hint.setText("点一下就连过去");
        for (final Discovery.Device d : found) {
            Button b = makeButton(d.name + "\n" + d.host + ":" + d.port, false);
            b.setAllCaps(false);
            b.setGravity(Gravity.START | Gravity.CENTER_VERTICAL);
            b.setTextSize(TypedValue.COMPLEX_UNIT_SP, 15);
            b.setLineSpacing(dp(4), 1f);
            b.setPadding(dp(16), dp(12), dp(16), dp(12));
            LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                    ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
            lp.bottomMargin = dp(10);
            b.setOnClickListener(new View.OnClickListener() {
                @Override
                public void onClick(View v) {
                    openWeb("http://" + d.host + ":" + d.port + "/");
                }
            });
            deviceList.addView(b, lp);
        }
    }

    /* ---------------- 网页 ---------------- */

    /**
     * 上次连过的端口。电脑的默认端口被占用时会顺延，所以网段扫描也要试一下它。
     * 解析失败返回 0。
     */
    private int lastPort() {
        String url = Prefs.url(this);
        if (url == null) return 0;
        try {
            int port = new java.net.URL(url).getPort();
            return port > 0 ? port : 0;
        } catch (Exception e) {
            return 0;
        }
    }

    private void openWeb(String url) {
        if (web == null) {
            web = new WebView(this);
            web.setBackgroundColor(C_BG);

            WebSettings s = web.getSettings();
            s.setJavaScriptEnabled(true);
            s.setDomStorageEnabled(true);
            s.setAllowFileAccess(false);
            s.setAllowContentAccess(false);
            s.setMixedContentMode(WebSettings.MIXED_CONTENT_ALWAYS_ALLOW);
            s.setUserAgentString(s.getUserAgentString() + " PastePort/0.1");

            web.addJavascriptInterface(new Bridge(this), "PastePort");
            web.setWebViewClient(new WebViewClient());
            web.setOverScrollMode(View.OVER_SCROLL_NEVER);
        }
        setContentView(web);
        String target = url.endsWith("/") ? url : url + "/";
        if (!target.equals(web.getUrl())) {
            web.loadUrl(target);
        }
    }

    @Override
    public void onBackPressed() {
        if (web != null && web.canGoBack()) {
            web.goBack();
            return;
        }
        super.onBackPressed();
    }

    /* ---------------- 小工具 ---------------- */

    private static String normalize(String raw) {
        String s = raw.trim();
        if (!s.startsWith("http://") && !s.startsWith("https://")) s = "http://" + s;
        int schemeEnd = s.indexOf("://") + 3;
        String head = s.substring(0, schemeEnd);
        String rest = s.substring(schemeEnd);
        int slash = rest.indexOf('/');
        String hostPort = slash < 0 ? rest : rest.substring(0, slash);
        if (!hostPort.contains(":")) hostPort = hostPort + ":8787";
        return head + hostPort + "/";
    }

    private Button makeButton(String text, boolean primary) {
        Button b = new Button(this);
        b.setText(text);
        b.setTextSize(TypedValue.COMPLEX_UNIT_SP, 14);
        b.setAllCaps(false);
        b.setTextColor(primary ? 0xFF04231F : C_FG);
        if (primary) {
            GradientDrawable g = new GradientDrawable();
            g.setColor(C_ACC);
            g.setCornerRadius(dp(11));
            b.setBackground(g);
        } else {
            b.setBackground(makeCard(12, C_CARD, C_LINE));
        }
        b.setStateListAnimator(null);
        return b;
    }

    private GradientDrawable makeCard(int radiusDp, int fill, int stroke) {
        GradientDrawable g = new GradientDrawable();
        g.setColor(fill);
        g.setCornerRadius(dp(radiusDp));
        g.setStroke(Math.max(1, dp(1)), stroke);
        return g;
    }

    private int dp(float v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }

    private void toast(String msg) {
        Toast.makeText(this, msg, Toast.LENGTH_SHORT).show();
    }
}

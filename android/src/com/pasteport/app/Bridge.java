package com.pasteport.app;

import android.app.Activity;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.os.Handler;
import android.os.Looper;
import android.webkit.JavascriptInterface;
import android.widget.Toast;

/**
 * 暴露给网页的 JS 接口（window.PastePort）。
 * 网页跑在 http:// 局域网地址上，属于非安全上下文，navigator.clipboard 会被浏览器禁用，
 * 所以「复制到剪贴板」这件事交给原生来做——这也是 APK 比浏览器更好用的地方之一。
 */
public class Bridge {

    private final Activity activity;
    private final Handler ui = new Handler(Looper.getMainLooper());

    public Bridge(Activity activity) {
        this.activity = activity;
    }

    @JavascriptInterface
    public void copy(final String text) {
        try {
            ClipboardManager cm = (ClipboardManager) activity.getSystemService(Context.CLIPBOARD_SERVICE);
            if (cm != null) {
                cm.setPrimaryClip(ClipData.newPlainText("PastePort", text == null ? "" : text));
            }
        } catch (Exception ignored) {
            // 某些 ROM 在后台限制剪贴板，忽略即可
        }
    }

    @JavascriptInterface
    public void remember(final String token, final String origin) {
        Prefs.save(activity, origin, token);
    }

    @JavascriptInterface
    public void toast(final String message) {
        ui.post(new Runnable() {
            @Override
            public void run() {
                Toast.makeText(activity, message, Toast.LENGTH_SHORT).show();
            }
        });
    }
}

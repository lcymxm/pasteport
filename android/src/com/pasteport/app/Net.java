package com.pasteport.app;

import org.json.JSONObject;

import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;

/** 分享菜单发文字时用的最小 HTTP 客户端。 */
public final class Net {

    private Net() {
    }

    /** 把一段文字 POST 到电脑的剪贴板。成功返回 true。 */
    public static boolean postClip(String base, String token, String text) {
        HttpURLConnection conn = null;
        try {
            String url = (base.endsWith("/") ? base : base + "/") + "api/clip";
            JSONObject body = new JSONObject();
            body.put("text", text);
            body.put("from", "android");
            body.put("name", "手机");
            byte[] payload = body.toString().getBytes("UTF-8");

            conn = (HttpURLConnection) new URL(url).openConnection();
            conn.setRequestMethod("POST");
            conn.setConnectTimeout(5000);
            conn.setReadTimeout(8000);
            conn.setDoOutput(true);
            conn.setFixedLengthStreamingMode(payload.length);
            conn.setRequestProperty("Content-Type", "application/json; charset=utf-8");
            conn.setRequestProperty("X-PP-Token", token);

            OutputStream os = conn.getOutputStream();
            os.write(payload);
            os.flush();
            os.close();

            int code = conn.getResponseCode();
            return code >= 200 && code < 300;
        } catch (Exception e) {
            return false;
        } finally {
            if (conn != null) conn.disconnect();
        }
    }
}

package com.pasteport.app;

import android.content.Context;
import android.content.SharedPreferences;

/** 只存两样东西：电脑地址和令牌。 */
public final class Prefs {

    private static final String FILE = "pasteport";
    private static final String K_URL = "url";
    private static final String K_TOKEN = "token";

    private Prefs() {
    }

    private static SharedPreferences sp(Context c) {
        return c.getSharedPreferences(FILE, Context.MODE_PRIVATE);
    }

    public static void save(Context c, String origin, String token) {
        if (origin == null || token == null) return;
        String url = origin.endsWith("/") ? origin : origin + "/";
        sp(c).edit().putString(K_URL, url).putString(K_TOKEN, token).apply();
    }

    public static String url(Context c) {
        return sp(c).getString(K_URL, null);
    }

    public static String token(Context c) {
        return sp(c).getString(K_TOKEN, null);
    }

    public static void clear(Context c) {
        sp(c).edit().remove(K_URL).remove(K_TOKEN).apply();
    }
}

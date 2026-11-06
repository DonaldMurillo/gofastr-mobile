package dev.gofastr.nativeui;

import android.app.PendingIntent;
import android.appwidget.AppWidgetManager;
import android.appwidget.AppWidgetProvider;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import android.content.res.Configuration;
import android.graphics.Color;
import android.net.Uri;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.widget.RemoteViews;
import org.json.JSONArray;
import org.json.JSONObject;
import java.nio.charset.StandardCharsets;
import java.util.HashSet;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.function.Consumer;

/** A public, bounded snapshot rendered without starting the Go runtime. */
public final class HomeWidget extends AppWidgetProvider {
    private static final String STORE = "gofastr.widget";
    private static final String SNAPSHOT = "snapshot";
    public static final String SCHEME_METADATA = "dev.gofastr.nativeui.widgetScheme";
    private static final ExecutorService worker = Executors.newSingleThreadExecutor();
    private static SharedPreferences store(Context context) { return context.getSharedPreferences(STORE, Context.MODE_PRIVATE); }

    public static boolean available(Context context) {
        try {
            ApplicationInfo info = context.getPackageManager().getApplicationInfo(context.getPackageName(), PackageManager.GET_META_DATA);
            String scheme = info.metaData == null ? "" : info.metaData.getString(SCHEME_METADATA, "");
            if (!scheme.matches("[a-z][a-z0-9+.-]{0,63}")) return false;
            context.getPackageManager().getReceiverInfo(new ComponentName(context, HomeWidget.class), 0);
            return true;
        } catch (Exception failure) { return false; }
    }

    /** Completion is delivered once on main. No request asks the launcher to pin a widget. */
    public static void perform(Context context, JSONObject request, Consumer<JSONObject> completion) {
        Context app = context.getApplicationContext();
        JSONObject operation = JSON.copy(request);
        Handler main = new Handler(Looper.getMainLooper());
        worker.execute(() -> {
            JSONObject result;
            try {
                if (!available(app)) result = JSON.object("status", "unsupported");
                else {
                    String action = operation.getString("operation");
                    JSONObject snapshot = operation.optJSONObject(SNAPSHOT);
                    if ("publish".equals(action)) {
                        validate(snapshot);
                        if (!store(app).edit().putString(SNAPSHOT, snapshot.toString()).commit()) throw new IllegalStateException("Widget snapshot could not be stored");
                    } else if ("clear".equals(action) || "status".equals(action)) {
                        if (operation.has(SNAPSHOT) && !operation.isNull(SNAPSHOT)) throw new IllegalArgumentException("Only publishing accepts widget content");
                        if ("clear".equals(action) && !store(app).edit().remove(SNAPSHOT).commit()) throw new IllegalStateException("Widget snapshot could not be removed");
                    } else throw new IllegalArgumentException("Unknown widget operation");
                    boolean published = read(app) != null;
                    if (!"status".equals(action)) updateAll(app);
                    result = JSON.object("status", "completed", "widget", JSON.object("published", published));
                }
            } catch (Exception failure) {
                // Do not echo widget content or storage paths into service errors.
                result = JSON.object("status", "failed", "message", "The widget snapshot could not be updated.");
            }
            JSONObject answer = result;
            main.post(() -> completion.accept(answer));
        });
    }

    private static JSONObject read(Context context) throws Exception {
        String saved = store(context).getString(SNAPSHOT, null);
        if (saved == null) return null;
        if (saved.getBytes(StandardCharsets.UTF_8).length > 16384) throw new IllegalArgumentException("Widget snapshot exceeds 16 KiB");
        JSONObject value = new JSONObject(saved);
        validate(value);
        return value;
    }

    static void validate(JSONObject snapshot) throws Exception {
        if (snapshot == null || snapshot.toString().getBytes(StandardCharsets.UTF_8).length > 16384) throw new IllegalArgumentException("Missing or large widget snapshot");
        String title = snapshot.getString("title"), text = snapshot.optString("text", ""), route = snapshot.getString("route");
        if (title.isEmpty() || !text(title, 200, false) || !text(text, 2048, true) || !localRoute(route)) throw new IllegalArgumentException("Invalid widget content");
        JSONArray items = snapshot.optJSONArray("items");
        if (snapshot.has("items") && !snapshot.isNull("items") && items == null) throw new IllegalArgumentException("Invalid widget items");
        if (items == null) return;
        if (items.length() > 4) throw new IllegalArgumentException("Too many widget items");
        Set<String> identifiers = new HashSet<>();
        for (int index = 0; index < items.length(); index++) {
            JSONObject item = items.getJSONObject(index);
            String id = item.getString("id"), name = item.getString("title");
            if (!id.matches("[A-Za-z0-9._:-]{1,64}") || !identifiers.add(id) || name.isEmpty() || !text(name, 200, false) || !text(item.optString("subtitle", ""), 512, true)) throw new IllegalArgumentException("Invalid widget item");
        }
    }

    private static boolean text(String value, int maximum, boolean multiline) {
        if (value.getBytes(StandardCharsets.UTF_8).length > maximum) return false;
        for (int index = 0; index < value.length(); index++) {
            char character = value.charAt(index);
            if (Character.isHighSurrogate(character)) {
                if (++index >= value.length() || !Character.isLowSurrogate(value.charAt(index))) return false;
            } else if (Character.isLowSurrogate(character) || Character.isISOControl(character) && !(multiline && (character == '\n' || character == '\t'))) return false;
        }
        return true;
    }

    private static boolean localRoute(String route) {
        if (!text(route, 2048, false) || !route.startsWith("/") || route.startsWith("//") || route.contains("\\") || route.contains("#")) return false;
        for (int offset = 0; offset < route.length(); offset++) if (Character.isWhitespace(route.charAt(offset)) || Character.isSpaceChar(route.charAt(offset))) return false;
        try {
            java.net.URI parsed = new java.net.URI(route);
            String path = parsed.getPath();
            String decoded = Uri.decode(route);
            if (parsed.isAbsolute() || parsed.getRawAuthority() != null || path == null || path.contains("//") || path.contains("\\") || path.contains("?") || path.contains("#") || parsed.getRawPath().toLowerCase(java.util.Locale.ROOT).contains("%2f") || !text(decoded, 2048, false)) return false;
            for (String segment : path.split("/", -1)) if (segment.equals(".") || segment.equals("..")) return false;
            return true;
        } catch (Exception failure) { return false; }
    }

    private static void updateAll(Context context) {
        AppWidgetManager manager = AppWidgetManager.getInstance(context);
        for (int identifier : manager.getAppWidgetIds(new ComponentName(context, HomeWidget.class))) update(context, manager, identifier);
    }

    private static void update(Context context, AppWidgetManager manager, int identifier) {
        JSONObject snapshot = null;
        try { snapshot = read(context); } catch (Exception ignored) { /* Render a safe empty state if storage is damaged. */ }
        String title = context.getApplicationInfo().loadLabel(context.getPackageManager()).toString();
        String summary = "Open the app to choose widget content.";
        String route = "/";
        if (snapshot != null) {
            title = snapshot.optString("title"); route = snapshot.optString("route");
            StringBuilder body = new StringBuilder(snapshot.optString("text", ""));
            JSONArray items = JSON.array(snapshot, "items");
            for (int index = 0; index < items.length(); index++) {
                JSONObject item = items.optJSONObject(index);
                if (item == null) continue;
                if (body.length() > 0) body.append('\n');
                body.append(item.optString("title"));
                String subtitle = item.optString("subtitle", "");
                if (!subtitle.isEmpty()) body.append(": ").append(subtitle);
            }
            summary = body.toString();
        }
        int layout=context.getResources().getIdentifier("gofastr_widget", "layout", context.getPackageName());
        if(layout==0)throw new IllegalStateException("Widget layout was not packaged");
        RemoteViews views = new RemoteViews(context.getPackageName(), layout);
        views.setTextViewText(android.R.id.text1, title);
        views.setTextViewText(android.R.id.text2, summary);
        views.setInt(android.R.id.text1, "setMaxLines", 1);
        views.setBoolean(android.R.id.text2, "setSingleLine", false);
        int height = manager.getAppWidgetOptions(identifier).getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_HEIGHT, 110);
        views.setInt(android.R.id.text2, "setMaxLines", Math.max(2, Math.min(12, (height - 35) / 18)));
        boolean dark = (context.getResources().getConfiguration().uiMode & Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES;
        for (int id : new int[]{android.R.id.text1, android.R.id.text2}) {
            views.setTextColor(id, dark ? Color.WHITE : Color.BLACK);
            views.setInt(id, "setBackgroundColor", dark ? Color.rgb(32, 33, 36) : Color.WHITE);
        }
        views.setInt(android.R.id.content, "setBackgroundColor", dark ? Color.rgb(32, 33, 36) : Color.WHITE);
        try {
            ApplicationInfo info = context.getPackageManager().getApplicationInfo(context.getPackageName(), PackageManager.GET_META_DATA);
            String scheme = info.metaData == null ? "" : info.metaData.getString(SCHEME_METADATA, "");
            Intent launch = context.getPackageManager().getLaunchIntentForPackage(context.getPackageName());
            if (launch != null && scheme.matches("[a-z][a-z0-9+.-]{0,63}")) {
                launch.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
                if (snapshot == null) launch.setAction(Intent.ACTION_MAIN).setData(null);
                else launch.setAction(Intent.ACTION_VIEW).setData(Uri.parse(scheme + ":" + route));
                PendingIntent tap = PendingIntent.getActivity(context, 0, launch, PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
                views.setOnClickPendingIntent(android.R.id.text1, tap);
                views.setOnClickPendingIntent(android.R.id.text2, tap);
                views.setOnClickPendingIntent(android.R.id.content, tap);
            }
        } catch (Exception ignored) { /* The visible snapshot remains useful without an app launch target. */ }
        manager.updateAppWidget(identifier, views);
    }

    @Override public void onUpdate(Context context, AppWidgetManager manager, int[] identifiers) {
        for (int identifier : identifiers) update(context, manager, identifier);
    }
    @Override public void onAppWidgetOptionsChanged(Context context, AppWidgetManager manager, int identifier, Bundle options) {
        update(context, manager, identifier);
    }
}

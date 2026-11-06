package dev.gofastr.nativeui;

import android.content.res.ColorStateList;
import android.graphics.Canvas;
import android.graphics.ColorFilter;
import android.graphics.Paint;
import android.graphics.Path;
import android.graphics.PixelFormat;
import android.graphics.Typeface;
import android.graphics.drawable.Drawable;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.RippleDrawable;
import android.view.View;
import android.widget.EditText;
import android.widget.CheckBox;
import android.widget.Switch;
import android.widget.TextView;
import java.util.Map;
import java.util.WeakHashMap;

/** Semantic styling reapplies to retained controls without rebuilding editors. */
final class Appearance {
    enum Tone { BODY, SECONDARY, HEADING, SECTION, DOCUMENT_TITLE, DOCUMENT_BODY, SEARCH, INPUT, SURFACE, SUBTLE, ACTION, PRIMARY, OUTLINE }

    private final GofastrActivity host;
    private final Map<View, Tone> tones = new WeakHashMap<>();

    Appearance(GofastrActivity host) { this.host = host; }

    int surface() { return host.dark() ? 0xff262522 : 0xffffffff; }
    int subtle() { return host.dark() ? 0xff302e29 : 0xffeeece6; }
    int border() { return host.dark() ? 0xff454139 : 0xffdedad1; }
    int onAccent() { return host.dark() ? 0xff382600 : 0xffffffff; }

    void apply(View view, Tone tone) {
        tones.put(view, tone);
        style(view, tone);
    }

    void refresh() {
        for (Map.Entry<View, Tone> entry : tones.entrySet()) style(entry.getKey(), entry.getValue());
    }

    private void style(View view, Tone tone) {
        if (view instanceof TextView) {
            TextView text = (TextView) view;
            text.setTextColor(tone == Tone.SECONDARY || tone == Tone.SECTION ? host.muted() : host.ink());
            text.setHintTextColor(host.muted());
            if (tone == Tone.PRIMARY) text.setTextColor(onAccent());
            if (tone == Tone.ACTION) text.setTextColor(host.accent());
            int iconColor = tone == Tone.PRIMARY ? onAccent() : tone == Tone.ACTION ? host.accent() : host.muted();
            for (Drawable icon : text.getCompoundDrawables()) if (icon != null) icon.setTint(iconColor);
            if (view instanceof EditText) {
                text.setBackgroundTintList(null);
                text.setHighlightColor(host.dark() ? 0x66ffcc68 : 0x44925500);
            }
        }
        ColorStateList checked = new ColorStateList(new int[][] { new int[] { android.R.attr.state_checked }, new int[] {} },
                new int[] { host.accent(), host.muted() });
        if (view instanceof CheckBox) ((CheckBox) view).setButtonTintList(checked);
        if (view instanceof Switch) {
            ((Switch) view).setThumbTintList(checked);
            ((Switch) view).setTrackTintList(checked.withAlpha(80));
        }
        switch (tone) {
            case SURFACE: case INPUT:
                view.setBackground(shape(surface(), 18, 0)); break;
            case SEARCH: case SUBTLE:
                view.setBackground(shape(subtle(), 16, 0)); break;
            case PRIMARY:
                view.setBackground(ripple(host.accent(), 24, 0)); break;
            case OUTLINE:
                view.setBackground(ripple(surface(), 14, border())); break;
            default: break;
        }
    }

    GradientDrawable shape(int color, int radius, int stroke) {
        GradientDrawable drawable = new GradientDrawable();
        drawable.setColor(color);
        drawable.setCornerRadius(host.dp(radius));
        if (stroke != 0) drawable.setStroke(host.dp(1), stroke);
        return drawable;
    }

    Drawable ripple(int color, int radius, int stroke) {
        return new RippleDrawable(ColorStateList.valueOf(host.dark() ? 0x24ffffff : 0x18000000),
                shape(color, radius, stroke), shape(0xffffffff, radius, 0));
    }

    void typography(TextView text, Tone tone) {
        apply(text, tone);
        text.setIncludeFontPadding(false);
        text.setLineSpacing(host.dp(3), 1);
        switch (tone) {
            case HEADING:
                text.setTextSize(34); text.setTypeface(Typeface.create("sans-serif", Typeface.BOLD)); break;
            case DOCUMENT_TITLE:
                text.setTextSize(30); text.setTypeface(Typeface.create("sans-serif", Typeface.BOLD)); break;
            case DOCUMENT_BODY:
                text.setTextSize(18); text.setLineSpacing(host.dp(6), 1); break;
            case SECTION:
                text.setTextSize(13); text.setTypeface(Typeface.create("sans-serif-medium", Typeface.NORMAL)); break;
            case SECONDARY:
                text.setTextSize(13); break;
            default: break;
        }
    }

    Drawable icon(String name, int color, int size) {
        Drawable icon = new Symbol(name, color);
        icon.setBounds(0, 0, host.dp(size), host.dp(size));
        return icon;
    }

    /** Small library-owned semantic icons, drawn at the device's resolution. */
    private static final class Symbol extends Drawable {
        private final String name;
        private final Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);

        Symbol(String name, int color) {
            this.name = name;
            paint.setColor(color);
            paint.setStyle(Paint.Style.STROKE);
            paint.setStrokeWidth(1.7f);
            paint.setStrokeCap(Paint.Cap.ROUND);
            paint.setStrokeJoin(Paint.Join.ROUND);
        }

        private void line(Canvas canvas, float... points) {
            Path path = new Path(); path.moveTo(points[0], points[1]);
            for (int i = 2; i < points.length; i += 2) path.lineTo(points[i], points[i + 1]);
            canvas.drawPath(path, paint);
        }

        @Override public void draw(Canvas canvas) {
            int saved = canvas.save();
            canvas.translate(getBounds().left, getBounds().top);
            canvas.scale(getBounds().width() / 24f, getBounds().height() / 24f);
            switch (name) {
                case "back": line(canvas, 15,5, 8,12, 15,19); break;
                case "chevron": line(canvas, 9,6, 15,12, 9,18); break;
                case "down": line(canvas, 6,9, 12,15, 18,9); break;
                case "compose":
                    line(canvas, 14,4, 4,4, 4,20, 20,20, 20,10);
                    line(canvas, 10,14, 11,10, 19,2, 22,5, 14,13, 10,14); break;
                case "search":
                    canvas.drawCircle(10.5f,10.5f,6.5f,paint); line(canvas, 16,16, 21,21); break;
                case "info":
                    canvas.drawCircle(12,12,9,paint); line(canvas, 12,11, 12,17); line(canvas, 12,7, 12,7.2f); break;
                case "more":
                    canvas.drawCircle(12,5,0.7f,paint); canvas.drawCircle(12,12,0.7f,paint); canvas.drawCircle(12,19,0.7f,paint); break;
                case "file": case "files": case "folder":
                    line(canvas, 3,7, 3,5, 9,5, 11,7, 21,7, 21,20, 3,20, 3,7); break;
                case "settings":
                    canvas.drawCircle(12,12,4,paint);
                    for (int i=0;i<8;i++) { canvas.save(); canvas.rotate(i*45,12,12); line(canvas, 12,2, 12,5); canvas.restore(); }
                    canvas.drawCircle(12,12,8,paint); break;
                case "photo":
                    canvas.drawRoundRect(3,3,21,21,3,3,paint); canvas.drawCircle(8,8,1.5f,paint);
                    line(canvas, 4,18, 10,12, 14,16, 17,12, 21,16); break;
                case "audio":
                    line(canvas, 4,10, 4,14); line(canvas, 8,6, 8,18); line(canvas, 12,3, 12,21);
                    line(canvas, 16,7, 16,17); line(canvas, 20,10, 20,14); break;
                default:
                    line(canvas, 14,3, 5,3, 5,21, 19,21, 19,8, 14,3, 14,8, 19,8);
                    line(canvas, 9,12, 15,12); line(canvas, 9,16, 15,16); break;
            }
            canvas.restoreToCount(saved);
        }

        @Override public void setAlpha(int alpha) { paint.setAlpha(alpha); invalidateSelf(); }
        @Override public void setColorFilter(ColorFilter filter) { paint.setColorFilter(filter); invalidateSelf(); }
        @Override public int getOpacity() { return PixelFormat.TRANSLUCENT; }
    }
}

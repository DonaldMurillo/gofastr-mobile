package dev.gofastr.nativeui;

import android.animation.Animator;
import android.animation.AnimatorListenerAdapter;
import android.animation.ValueAnimator;
import android.graphics.Typeface;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewConfiguration;
import android.view.animation.DecelerateInterpolator;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.HorizontalScrollView;
import android.widget.LinearLayout;
import org.json.JSONArray;
import org.json.JSONObject;
import java.util.function.BooleanSupplier;
import java.util.function.Consumer;

/** A row that follows a horizontal drag and exposes declared Go actions. */
final class SwipeRow extends FrameLayout {
    private final GofastrActivity host;
    private final View content;
    private final JSONArray leadingActions, trailingActions;
    private final BooleanSupplier allowed;
    private final Consumer<JSONObject> perform;
    private final Consumer<SwipeRow> revealed;
    private final HorizontalScrollView leading, trailing;
    private final int slop;
    private float downX, downY, downOffset, offset;
    private boolean dragging, vertical, touchingRail;
    private ValueAnimator animation;
    private int animationGeneration;

    SwipeRow(GofastrActivity host, View content, String rowID, JSONArray leadingActions,
             JSONArray trailingActions, BooleanSupplier allowed, Consumer<JSONObject> perform,
             Consumer<SwipeRow> revealed) {
        super(host);
        this.host = host;
        this.content = content;
        this.leadingActions = leadingActions;
        this.trailingActions = trailingActions;
        this.allowed = allowed;
        this.perform = perform;
        this.revealed = revealed;
        slop = ViewConfiguration.get(host).getScaledTouchSlop();
        host.appearance.apply(this, Appearance.Tone.SURFACE);
        setClipToOutline(true);
        content.setMinimumHeight(Math.max(content.getMinimumHeight(),
                host.dp(Math.round(64 * getResources().getConfiguration().fontScale))));
        leading = rail(leadingActions, rowID, true);
        trailing = rail(trailingActions, rowID, false);
        addView(leading, new LayoutParams(0, -1, Gravity.START));
        addView(trailing, new LayoutParams(0, -1, Gravity.END));
        addView(content, new LayoutParams(-1, -2));
        updateOffset(0);
    }

    private HorizontalScrollView rail(JSONArray actions, String rowID, boolean start) {
        HorizontalScrollView rail = new HorizontalScrollView(host);
        rail.setFillViewport(true);
        rail.setHorizontalScrollBarEnabled(false);
        rail.setOverScrollMode(OVER_SCROLL_NEVER);
        LinearLayout buttons = host.row();
        buttons.setMinimumWidth(host.dp(88) * actions.length());
        for (int i = 0; i < actions.length(); i++) {
            JSONObject action = actions.optJSONObject(i);
            boolean destructive = action.optString("role").equals("destructive");
            int color = actionColor(action, start);
            int ink = start && !destructive ? host.appearance.onAccent() : 0xffffffff;
            Button button = host.button(action.optString("label"),
                    "native.row.action." + rowID + "." + action.optString("id"), v -> execute(action));
            button.setTypeface(Typeface.create("sans-serif-medium", Typeface.NORMAL));
            button.setTextSize(13);
            button.setMaxLines(3);
            button.setEllipsize(android.text.TextUtils.TruncateAt.END);
            button.setContentDescription(action.optString("label"));
            button.setTextColor(ink);
            button.setGravity(Gravity.CENTER);
            button.setPadding(host.dp(10), host.dp(10), host.dp(10), host.dp(10));
            button.setBackground(host.appearance.ripple(color, 0, 0));
            button.setImportantForAccessibility(IMPORTANT_FOR_ACCESSIBILITY_YES);
            buttons.addView(button, new LinearLayout.LayoutParams(0, -1, 1));
        }
        if (actions.length() > 0) rail.setBackgroundColor(actionColor(actions.optJSONObject(0), start));
        rail.addView(buttons, new HorizontalScrollView.LayoutParams(-2, -1));
        return rail;
    }

    private int actionColor(JSONObject action, boolean start) {
        if (action.optString("role").equals("destructive")) return host.dark() ? 0xffb6363d : 0xffb32632;
        if (start) return host.accent();
        return host.dark() ? 0xff5b615d : 0xff53645b;
    }

    private int direction() { return getLayoutDirection() == LAYOUT_DIRECTION_RTL ? -1 : 1; }
    private boolean isLeading(float value) { return value * direction() > 0; }
    private JSONArray actions(float value) { return isLeading(value) ? leadingActions : trailingActions; }

    private int railWidth(JSONArray actions) {
        return Math.min(host.dp(88) * actions.length(), Math.max(0, getWidth() - host.dp(56)));
    }

    private boolean fullSwipe(JSONArray actions) {
        if (actions.length() == 0) return false;
        JSONObject first = actions.optJSONObject(0);
        return first.optBoolean("fullSwipe") && !first.optString("role").equals("destructive");
    }

    private void updateOffset(float value) {
        offset = value;
        content.setTranslationX(value);
        showRail(leading, leadingActions, isLeading(value) && value != 0);
        showRail(trailing, trailingActions, !isLeading(value) && value != 0);
    }

    private void showRail(HorizontalScrollView rail, JSONArray actions, boolean visible) {
        rail.setVisibility(visible ? VISIBLE : INVISIBLE);
        rail.setImportantForAccessibility(visible ? IMPORTANT_FOR_ACCESSIBILITY_AUTO : IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS);
        if (!visible) return;
        int width = Math.max(railWidth(actions), Math.round(Math.abs(offset)));
        LayoutParams layout = (LayoutParams) rail.getLayoutParams();
        if (layout.width != width) {
            layout.width = width;
            rail.setLayoutParams(layout);
        }
    }

    void close() { animateTo(0); }

    private void execute(JSONObject action) {
        if (!isAttachedToWindow() || !allowed.getAsBoolean()) return;
        cancelAnimation();
        updateOffset(0);
        perform.accept(action);
    }

    private void cancelAnimation() {
        animationGeneration++;
        if (animation != null) { animation.cancel(); animation = null; }
    }

    private void animateTo(float target) {
        cancelAnimation();
        int generation = animationGeneration;
        animation = ValueAnimator.ofFloat(offset, target);
        animation.setDuration(190);
        animation.setInterpolator(new DecelerateInterpolator());
        animation.addUpdateListener(value -> updateOffset((float) value.getAnimatedValue()));
        animation.addListener(new AnimatorListenerAdapter() {
            @Override public void onAnimationEnd(Animator animator) {
                if (generation != animationGeneration) return;
                animation = null;
            }
        });
        animation.start();
    }

    @Override public boolean onInterceptTouchEvent(MotionEvent event) {
        if (!allowed.getAsBoolean()) return false;
        switch (event.getActionMasked()) {
            case MotionEvent.ACTION_DOWN:
                cancelAnimation();
                downX = event.getX(); downY = event.getY(); downOffset = offset;
                dragging = false; vertical = false;
                touchingRail = offset > 0 ? downX < offset : offset < 0 && downX > getWidth() + offset;
                return offset != 0 && !touchingRail;
            case MotionEvent.ACTION_MOVE:
                if (touchingRail || vertical) return false;
                detectDrag(event);
                return dragging;
            default:
                return false;
        }
    }

    private void detectDrag(MotionEvent event) {
        float dx = event.getX() - downX, dy = event.getY() - downY;
        if (dragging || vertical) return;
        if (Math.abs(dy) > slop && Math.abs(dy) > Math.abs(dx)) { vertical = true; return; }
        if (Math.abs(dx) <= slop || Math.abs(dx) < Math.abs(dy) * 1.25f) return;
        dragging = true;
        getParent().requestDisallowInterceptTouchEvent(true);
        revealed.accept(this);
    }

    @Override public boolean onTouchEvent(MotionEvent event) {
        if (!allowed.getAsBoolean()) { close(); return true; }
        switch (event.getActionMasked()) {
            case MotionEvent.ACTION_DOWN:
                return true;
            case MotionEvent.ACTION_MOVE:
                detectDrag(event);
                if (dragging) {
                    float desired = downOffset + event.getX() - downX;
                    JSONArray actions = actions(desired);
                    float limit = railWidth(actions);
                    if (fullSwipe(actions)) limit = getWidth() * .95f;
                    updateOffset(Math.copySign(Math.min(Math.abs(desired), limit), desired));
                }
                return true;
            case MotionEvent.ACTION_UP:
                getParent().requestDisallowInterceptTouchEvent(false);
                if (!dragging) { close(); performClick(); return true; }
                JSONArray actions = actions(offset);
                if (Math.abs(offset) > getWidth() * .72f && fullSwipe(actions)) {
                    JSONObject first = actions.optJSONObject(0);
                    execute(first);
                } else {
                    float target = Math.abs(offset) > Math.min(host.dp(48), railWidth(actions) * .4f)
                            ? Math.copySign(railWidth(actions), offset) : 0;
                    animateTo(target);
                }
                dragging = false;
                return true;
            case MotionEvent.ACTION_CANCEL:
                getParent().requestDisallowInterceptTouchEvent(false);
                dragging = false;
                close();
                return true;
            default:
                return true;
        }
    }

    @Override public boolean performClick() { return super.performClick(); }

    @Override protected void onDetachedFromWindow() {
        cancelAnimation();
        updateOffset(0);
        super.onDetachedFromWindow();
    }
}

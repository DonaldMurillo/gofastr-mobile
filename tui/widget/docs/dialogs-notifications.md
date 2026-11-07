# Dialogs, notifications and button variants

Buttons accept `Variant: ButtonDefault`, `ButtonPrimary`, or `ButtonDanger`.
`ButtonDefault` is zero. `Style` overrides the variant's base style; hover,
focus and press styles keep their existing precedence. Disabled buttons use
`Theme.Muted`. `AutoFocused: true` requests focus when no control has it.

Dialogs are controlled by the caller. Put their element last in `Layers`:

```go
widget.Layers(
    screen,
    view.If(open, widget.Dialog{
        Title: "Save changes",
        Text:  "Save the current settings?",
        Buttons: []widget.DialogButton{
            {Label: "Cancel", OnClick: closeDialog},
            {Label: "Save", Variant: widget.ButtonPrimary, Default: true, OnClick: save},
        },
        OnClose: closeDialog,
    }.Element()),
)
```

`Body` replaces `Text` when its widget is non-nil. Text wraps within `Width`,
which limits the whole box and defaults to 60 columns. The first `Default`
action receives focus, or the first action when none is marked. Actions take
initial focus even if the body requests autofocus. Left and Right wrap among
actions; Tab traverses the modal's controls. Escape and backdrop presses call
`OnClose`. Callbacks decide when to close. Previous focus returns after removal.

`Confirm(title, text, confirmLabel string, onConfirm, onCancel func()) Dialog`
builds Cancel and a default primary confirm action. Escape and backdrop presses
call `onCancel`.

Keep a `Notifications` value in the caller's model and include `notices.Element()`
as a child of `Layers`. `Push(level, title, text) int` returns a stable ID.
`Dismiss(id)`, `Clear()`, `Len() int`, and `Items() []Notice` manage the notices.
`Items` returns a copy in oldest-first order. `Max` defaults to 5; pushing above
it drops the oldest items. IDs remain unique across `Clear`.

Notifications occupy the top-right corner with a one-cell margin. Each is at
most 40 columns wide including its border and padding. The newest is last.
Clicking dismisses only that notice. Notifications never take focus and contain
no clock; callers schedule expiry and dismiss IDs from their app loop. Use the
model on that loop, not concurrently from timer goroutines.

Theme additions are `ButtonPrimary`, `ButtonDanger`, `NoticeInfo`, `NoticeSuccess`,
`NoticeWarning` and `NoticeError`, all `cell.Style`. Notice styles color their
borders and titles. Leading glyphs are one column under both width methods.

## 2026-11-07 - Overlay hit bounds and initial focus
- Scope: tui/widget dialogs and notifications.
- Trigger: overlays must preserve clicks and focus beneath them.
- Approach: keep toast rectangles equal-width and contiguous; apply the margin through `position`, outside the hit rectangle. Visit dialog actions before Body for autofocus, then place Body above them visually.
- Evidence: `widget_test.go` covers real screens, keyed hover after removal, outside clicks, input focus, body autofocus and opaque surfaces. All 27 deliberate mutations failed for the intended assertion before restoration.
- Next time: keep hit bounds equal to painted toast bounds; clear overlay surfaces even when their style is zero.
- Status: active.

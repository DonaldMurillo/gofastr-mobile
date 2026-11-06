# Home-screen widgets

Widgets use one shared Go contract on Android and iOS. The app publishes a
small, public summary. The OS reads that saved summary without starting the Go
runtime or opening the database. Every installed widget shows the same summary.

Enable the native host with `--widgets --url-scheme notes-demo`. On iOS, also
provide `--app-group group.your.bundle.id`. The app and WidgetKit extension must
have that App Group entitlement. Widgets are disabled in builds that omit the
flag. WebView hosts do not advertise this capability.

Publish from an admitted Go action with a typed device service:

```go
nativeui.ServiceRequest{
    ID: "publish-summary",
    Widget: &widgets.Request{
        Operation: widgets.Publish,
        Snapshot: &widgets.Snapshot{
            Title: "Recent notes",
            Text:  "Last updated in the app",
            Route: "/notes",
            Items: []widgets.Item{
                {ID: "shopping", Title: "Shopping", Subtitle: "Milk and bread"},
            },
        },
    },
}
```

Use `widgets.Clear` to remove the saved summary, or `widgets.Status` to inspect
whether a valid summary is saved. Neither operation takes a snapshot. A
completed service has `Widget: &widgets.State{Published: true}` after publishing
and `Published: false` after clearing. Status reports either state. This state
does not report installed widget count or prove that the launcher has refreshed.
Publishing does not add a widget to the user's home screen. The user adds it
through the system widget picker.

The contract allows four items. Titles are at most 200 UTF-8 bytes, summary text
2,048 bytes, item subtitles 512 bytes, and the whole snapshot 16 KiB of JSON.
Item IDs are unique tokens of at most 64 ASCII characters. The snapshot has one
local route of at most 2,048 bytes. Both platforms open that route when tapped.
Items are summary text, with no separate actions. Normal Go navigation and
authorization rules still apply when the app opens. Publish again when the app
changes the data, or clear the snapshot when the user signs out.

Widget content can be visible outside the app. Do not include passwords,
tokens, private attachment bytes, or text that requires biometric authentication.
The iOS view marks its content as privacy sensitive so supported system contexts
can redact it, but that is not a promise that the summary is hidden everywhere.

## Android adapter

`HomeWidget` is an API 24 compatible `AppWidgetProvider`. It stores the bounded
JSON in private app preferences, redraws installed widgets after publish/clear,
and restores their summary when the launcher requests an update. A background
worker performs storage, and the service completes once on the main thread.
The bundled RemoteViews layout displays the title and a multiline summary.
Available height limits visible lines. Resize requests redraw the summary.

Packaging includes a receiver only when enabled, with an `APPWIDGET_UPDATE`
intent filter and `android.appwidget.provider` metadata pointing at
`@xml/gofastr_widget`. The provider XML uses
`@layout/gofastr_widget`, enables horizontal and vertical resizing,
and sets `updatePeriodMillis="0"`. The application metadata
`dev.gofastr.nativeui.widgetScheme` supplies the configured URL scheme.
Immutable pending intents explicitly launch the containing app with
`scheme:/local/route`. The renderer applies its normal departure guard.

## iOS adapter

`GofastrWidgets.swift` contains Foundation wire types and `MobileWidgetStore`.
The store atomically replaces `gofastr-widget-v1.json` in the App Group container,
using file protection until the first device unlock after restart. It requests
`WidgetCenter.reloadTimelines` for `dev.gofastr.summary`. WidgetKit controls when
that request takes effect. Completion reports stored content, not screen display.

`render/ios/widget-extension/GofastrWidgetExtension.swift` is a separate WidgetKit
executable. Compile it together with `GofastrWidgets.swift`, and embed the signed
result in the app's `PlugIns` directory. Do not compile its `@main` entry point
into the containing app. The separate `ios.WidgetSources` embedded filesystem
keeps these sources out of the normal renderer source collection. The extension
Info.plist needs `GofastrAppGroup` and `GofastrWidgetScheme`. Its extension point
is `com.apple.widgetkit-extension`. The app and extension share the App Group
entitlement. No Go framework is linked into the extension.

The widget supports small, medium, and large families. It validates and bounds
the file again before rendering. A missing, protected, or damaged file displays
an empty state. Its timeline has one entry and waits for the app to request a
reload. Background refresh and interactive mutations are not part of this
contract.

## Validation

`go test ./device/widgets ./render/ios` checks the Go validators. On macOS,
`TestWidgetSwiftContract` compiles and executes the actual Foundation DTOs against
Go-produced JSON, including false state values, duplicate IDs, unsafe routes,
and encoded path traversal. Android SDK compilation is part of
`go test ./cmd/gofastr-native`.

Android 14 x86-64 acceptance uses a separate AppWidgetHost to render the real
RemoteViews, follow its tap into the declared Go route, and observe clearing.
The host test caught that `TwoLineListItem` cannot be inflated by RemoteViews;
the bundled layout uses supported `LinearLayout` and `TextView` controls.

Windows cannot compile WidgetKit or run an iOS widget. Launcher/gallery
installation and resizing, protected-device behavior, other Android versions,
physical devices, and WidgetKit refresh timing still need platform acceptance.

Platform references: [Android app widgets](https://developer.android.com/develop/ui/views/appwidgets/overview),
[creating a WidgetKit extension](https://developer.apple.com/documentation/widgetkit/creating-a-widget-extension),
and [WidgetKit reload timing](https://developer.apple.com/documentation/widgetkit/keeping-a-widget-up-to-date).

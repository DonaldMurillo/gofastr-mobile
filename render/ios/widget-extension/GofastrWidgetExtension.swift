import Foundation
import SwiftUI
import WidgetKit

private enum GofastrWidgetConfiguration {
    static let kind = "dev.gofastr.summary"
    static var group: String { Bundle.main.object(forInfoDictionaryKey: "GofastrAppGroup") as? String ?? "" }
    static var scheme: String { Bundle.main.object(forInfoDictionaryKey: "GofastrWidgetScheme") as? String ?? "" }

    static func snapshot() -> MobileWidgetSnapshot? {
        guard !group.isEmpty, let directory = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) else { return nil }
        do {
            let bytes = try MobileWidgetStore.read(directory.appendingPathComponent(MobileWidgetStore.filename, isDirectory: false))
            let snapshot = try JSONDecoder().decode(MobileWidgetSnapshot.self, from: bytes)
            try snapshot.validate()
            return snapshot
        } catch { return nil }
    }

    static func destination(_ snapshot: MobileWidgetSnapshot?) -> URL? {
        guard !scheme.isEmpty, scheme.range(of: "^[a-z][a-z0-9+.-]{0,63}$", options: .regularExpression) != nil,
              let route = snapshot?.route, MobileWidgetSnapshot.localRoute(route) else { return nil }
        return URL(string: scheme + ":" + route)
    }
}

private struct GofastrWidgetEntry: TimelineEntry {
    let date: Date
    let snapshot: MobileWidgetSnapshot?
}

private struct GofastrWidgetProvider: TimelineProvider {
    func placeholder(in context: Context) -> GofastrWidgetEntry {
        GofastrWidgetEntry(date: Date(), snapshot: MobileWidgetSnapshot(title: "App summary", text: "Open the app to choose widget content.", route: "/", items: nil))
    }
    func getSnapshot(in context: Context, completion: @escaping (GofastrWidgetEntry) -> Void) {
        completion(GofastrWidgetEntry(date: Date(), snapshot: GofastrWidgetConfiguration.snapshot()))
    }
    func getTimeline(in context: Context, completion: @escaping (Timeline<GofastrWidgetEntry>) -> Void) {
        completion(Timeline(entries: [GofastrWidgetEntry(date: Date(), snapshot: GofastrWidgetConfiguration.snapshot())], policy: .never))
    }
}

private struct GofastrWidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: GofastrWidgetEntry

    private var summary: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(entry.snapshot?.title ?? (Bundle.main.object(forInfoDictionaryKey: "CFBundleDisplayName") as? String ?? "App summary"))
                .font(.headline).lineLimit(1)
            if let text = entry.snapshot?.text, !text.isEmpty {
                Text(text).font(.subheadline).lineLimit(family == .systemSmall ? 2 : 3)
            }
            ForEach(Array((entry.snapshot?.items ?? []).prefix(family == .systemSmall ? 1 : 4)), id: \.id) { item in
                VStack(alignment: .leading, spacing: 1) {
                    Text(item.title).font(.subheadline).lineLimit(1)
                    if let subtitle = item.subtitle, !subtitle.isEmpty {
                        Text(subtitle).font(.caption).foregroundColor(.secondary).lineLimit(1)
                    }
                }
            }
            if entry.snapshot == nil {
                Text("Open the app to choose widget content.").font(.subheadline)
            }
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .widgetURL(GofastrWidgetConfiguration.destination(entry.snapshot))
        .privacySensitive()
    }

    @ViewBuilder var body: some View {
        if #available(iOSApplicationExtension 17.0, *) {
            summary.containerBackground(.background, for: .widget)
        } else {
            summary.padding().background(Color(.systemBackground))
        }
    }
}

@main
struct GofastrSummaryWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: GofastrWidgetConfiguration.kind, provider: GofastrWidgetProvider()) { entry in
            GofastrWidgetView(entry: entry)
        }
        .configurationDisplayName("App summary")
        .description("Shows the summary published by the app. Tap to open it.")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge])
    }
}

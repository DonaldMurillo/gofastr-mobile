import Foundation

struct MobilePresentation: Decodable {
    let id: String
    let kind: String
    let title: String
    let message: String?
    let choices: [MobilePresentationChoice]
    let selection: MobilePresentationSelection?
    let dismissible: Bool
    let commit: Bool
}

struct MobilePresentationChoice: Decodable {
    let id: String
    let label: String
    let role: String?
    let disabled: Bool
}

struct MobilePresentationSelection: Decodable {
    let options: [MobilePresentationOption]
    let selected: [String]?
    let multiple: Bool
    let min: Int
    let max: Int
}

struct MobilePresentationOption: Decodable {
    let id: String
    let label: String
    let detail: String?
    let disabled: Bool
}

struct MobilePresentationSelectionState {
    let selection: MobilePresentationSelection?
    private var identifiers: Set<String>

    init(_ selection: MobilePresentationSelection?) {
        self.selection = selection
        identifiers = Set(selection?.selected ?? [])
    }

    var selected: [String] {
        selection?.options.filter { identifiers.contains($0.id) }.map { $0.id } ?? []
    }

    mutating func toggle(_ option: MobilePresentationOption) {
        guard let selection, !option.disabled else { return }
        if identifiers.contains(option.id) { identifiers.remove(option.id) }
        else if selection.multiple { identifiers.insert(option.id) }
        else { identifiers = [option.id] }
    }
}

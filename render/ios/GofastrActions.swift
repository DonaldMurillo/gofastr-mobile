import Foundation

struct MobileAction: Decodable {
    let id: String
    let label: String
    let icon: String?
    let leading: Bool?
    let primary: Bool?
}

struct MobileToolbarPlan {
    let leading: [MobileAction]
    let primary: MobileAction?
    let overflow: [MobileAction]

    init(_ actions: [MobileAction]) {
        leading = actions.filter { $0.leading == true }
        let trailing = actions.filter { $0.leading != true }
        let selected = trailing.first { $0.primary == true } ?? trailing.first
        primary = selected
        overflow = trailing.filter { $0.id != selected?.id }
    }
}

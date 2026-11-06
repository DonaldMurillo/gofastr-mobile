package ios

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIncomingSharesExecutableInbox(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("executable Foundation inbox checks require macOS")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("Xcode toolchain unavailable")
	}
	directory := t.TempDir()
	source, err := Sources.ReadFile("GofastrIncomingShares.swift")
	if err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(directory, "GofastrIncomingShares.swift")
	if err := os.WriteFile(library, source, 0600); err != nil {
		t.Fatal(err)
	}
	checks := filepath.Join(directory, "IncomingShareChecks.swift")
	if err := os.WriteFile(checks, []byte(incomingShareChecks), 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "incoming-share-checks")
	if output, err := exec.Command("xcrun", "--sdk", "macosx", "swiftc", "-parse-as-library", library, checks, "-o", executable).CombinedOutput(); err != nil {
		t.Fatalf("Swift inbox compile: %v\n%s", err, output)
	}
	if output, err := exec.Command(executable, filepath.Join(directory, "inbox")).CombinedOutput(); err != nil {
		t.Fatalf("Swift inbox behavior: %v\n%s", err, output)
	}
}

const incomingShareChecks = `
import Foundation
@main struct IncomingShareChecks {
 static func rejects(_ action: () throws -> Void) { do { try action(); fatalError("unsafe input accepted") } catch {} }
 static func check(_ value: @autoclosure () throws -> Bool) rethrows { let checked = try value(); precondition(checked) }
 static func main() throws {
  let directory = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
  let inbox = try MobileIncomingShareInbox(directory: directory)
  try check(try inbox.next() == nil)
  let id = String(repeating: "a", count: 32)
  let item = MobileIncomingShareItem(identifier: id, title: "Shared", text: "one", files: nil)
  try inbox.append(item)
  try check(try inbox.next()?.text == "one")
  try check(try inbox.next()?.identifier == id)
  try inbox.append(MobileIncomingShareItem(identifier: id, title: nil, text: "must not replace", files: nil))
  try check(try inbox.next()?.text == "one")
  try inbox.acknowledge(id)
  try inbox.acknowledge(id)
  try inbox.append(item)
  try check(try inbox.next() == nil)
  rejects { try inbox.acknowledge("../outside") }
  rejects { try MobileIncomingShareRequest(operation: "next", identifier: id).validate() }
  rejects { try MobileIncomingShareItem(identifier: id, title: nil, text: String(repeating: "x", count: 65_537), files: nil).validate() }
  rejects { try MobileIncomingShareItem(identifier: id, title: nil, text: nil, files: nil).validate() }
  rejects { _ = try MobileIncomingShareFile(name: "../private", mediaType: "text/plain", data: "").bytes() }
  for n in 0..<16 {
   let identifier = String(format: "%032x", n)
   try inbox.append(MobileIncomingShareItem(identifier: identifier, title: nil, text: "bounded", files: nil))
  }
  rejects { try inbox.append(MobileIncomingShareItem(identifier: String(repeating: "b", count: 32), title: nil, text: "overflow", files: nil)) }
  let first = directory.appendingPathComponent(String(format: "%032x", 0) + ".json")
  try FileManager.default.setAttributes([.modificationDate: Date().addingTimeInterval(-8 * 24 * 60 * 60)], ofItemAtPath: first.path)
  try inbox.append(MobileIncomingShareItem(identifier: String(repeating: "b", count: 32), title: nil, text: "after expiry", files: nil))
  precondition(!FileManager.default.fileExists(atPath: first.path))
 }
}
`

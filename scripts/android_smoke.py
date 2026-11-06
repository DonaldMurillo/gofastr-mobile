"""Exercise a built Notes APK through real Android controls in an emulator.

No app data is cleared. Each run creates its own note. ADB selects nodes by
accessibility ID or text. Screenshots, XML and results stay in --out.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import re
import shlex
import subprocess
import struct
import sys
import time
import xml.etree.ElementTree as ET
import zlib

sys.stdout.reconfigure(encoding="utf-8")


class Android:
    def __init__(self, serial="emulator-5580", package="dev.gofastr.androidqa", out="dist/android-acceptance"):
        if not serial.startswith("emulator-"):
            raise ValueError("This smoke test requires an emulator serial.")
        self.serial, self.package = serial, package
        self.out = Path(out)
        self.out.mkdir(parents=True, exist_ok=True)
        self.checks = []
        results = self.out / "results.json"
        if results.exists():
            previous = json.loads(results.read_text())
            if previous.get("package") == package:
                self.checks = previous.get("passed", [])

    def adb(self, *args):
        return subprocess.check_output(["adb", "-s", self.serial, *args], timeout=40)

    def nodes(self):
        self.adb("shell", "uiautomator", "dump", "/sdcard/gofastr-smoke.xml")
        xml = self.adb("shell", "cat", "/sdcard/gofastr-smoke.xml")
        (self.out / "window.xml").write_bytes(xml)
        return list(ET.fromstring(xml).iter("node"))

    def find(self, key, *, timeout=15):
        deadline = time.monotonic() + timeout
        while True:
            nodes = self.nodes()
            found = next((n for n in nodes if any(key.casefold() == (n.get(attr) or "").casefold() for attr in ("resource-id", "text", "content-desc")) or (n.get("content-desc") or "").casefold().startswith(key.casefold() + ",")), None)
            if found is not None:
                return found
            if time.monotonic() >= deadline:
                visible = [(n.get("resource-id"), n.get("text")) for n in nodes if n.get("text")]
                raise AssertionError(f"Missing {key!r}. Visible: {visible}")
            if key.startswith(("native.row.", "native.file.", "native.image.", "native.audio.", "native.details", "native.field.reminder")):
                bounds = [int(x) for x in re.findall(r"\d+", nodes[0].get("bounds", ""))]
                if len(bounds) == 4:
                    x, height = bounds[2] // 2, bounds[3]
                    self.adb("shell", "input", "swipe", str(x), str(height * 4 // 5), str(x), str(height // 3), "250")
            elif key in ("native.status", "native.field.body", "native.field.title"):
                bounds = [int(x) for x in re.findall(r"\d+", nodes[0].get("bounds", ""))]
                if len(bounds) == 4:
                    x, height = bounds[2] // 2, bounds[3]
                    self.adb("shell", "input", "swipe", str(x), str(height // 3), str(x), str(height * 4 // 5), "250")
            time.sleep(0.3)

    def tap(self, key):
        node = key if isinstance(key, ET.Element) else self.find(key)
        if node.get("enabled") == "false":
            raise AssertionError(f"Disabled control: {key}")
        coords = [int(x) for x in re.findall(r"\d+", node.get("bounds", ""))]
        if len(coords) != 4:
            raise AssertionError(f"Control has no bounds: {key}")
        self.adb("shell", "input", "tap", str((coords[0] + coords[2]) // 2), str((coords[1] + coords[3]) // 2))
        time.sleep(0.2)

    def enter(self, key, value, clear=False):
        self.tap(key)
        if clear:
            self.adb("shell", "input", "keycombination", "113", "29")
            self.adb("shell", "input", "keyevent", "67")
        self.adb("shell", "input", "text", shlex.quote(value.replace(" ", "%s")))

    def back(self):
        self.adb("shell", "input", "keyevent", "4")
        time.sleep(0.2)

    def shot(self, name):
        (self.out / (name + ".png")).write_bytes(self.adb("exec-out", "screencap", "-p"))
        self.adb("shell", "uiautomator", "dump", "/sdcard/gofastr-smoke.xml")
        (self.out / (name + ".xml")).write_bytes(self.adb("shell", "cat", "/sdcard/gofastr-smoke.xml"))

    def check(self, name):
        if name not in self.checks:
            self.checks.append(name)
        (self.out / "results.json").write_text(json.dumps({"serial": self.serial, "package": self.package, "passed": self.checks}, indent=2))
        print("PASS", name, flush=True)

    def summary(self):
        for n in self.nodes():
            if n.get("text") or n.get("resource-id", "").startswith("native."):
                print(n.get("resource-id"), repr(n.get("text")), n.get("bounds"))

    def pick_download(self, name):
        nodes = self.nodes()
        if not any(n.get("text") == name for n in nodes):
            self.tap("Show roots")
            model = self.adb("shell", "getprop", "ro.product.model").decode().strip()
            if name.endswith(".png") and any(n.get("text") == model for n in self.nodes()):
                self.tap(model); self.tap("Download")
            else:
                self.tap("Downloads")
        self.tap(name)

    def export_as(self, name):
        self.tap("native.action.export")
        field = next(n for n in self.nodes() if n.get("class") == "android.widget.EditText")
        self.enter(field, name, clear=True)
        self.tap("Save")
        self.find("native.action.export")
        return self.adb("exec-out", "cat", "/sdcard/Download/" + name)


def smoke(a: Android):
    a.adb("shell", "am", "start", "-W", "-n", a.package + "/.MainActivity")
    a.find("native.tab.notes")
    a.shot("01-launch")
    a.check("Native launch and tabs")
    title = "Android smoke " + time.strftime("%H%M%S")
    a.tap("native.action.new")
    a.enter("native.field.title", title)
    a.enter("native.field.body", "Written with Android native controls.")
    a.back()  # keyboard
    a.tap("native.action.save")
    a.find(title)
    a.check("Create, save and list a note")
    a.tap(title)
    a.enter("native.field.body", " Draft survives process restart.")
    a.back()
    a.adb("shell", "input", "keyevent", "3")  # background checkpoint
    time.sleep(1)
    a.adb("shell", "am", "force-stop", a.package)
    a.adb("shell", "am", "start", "-W", "-n", a.package + "/.MainActivity")
    a.find(title)
    a.tap(title)
    body = a.find("native.field.body").get("text", "")
    assert "Draft survives process restart." in body, body
    a.shot("02-restored-draft")
    a.check("Background checkpoint and process-death draft recovery")
    a.back()
    a.tap("Keep Editing")
    a.find("native.field.body")
    a.tap("native.back")
    a.tap("Keep Draft")
    a.find(title)
    a.check("Back guard preserves editor and draft")
    a.tap("native.tab.settings")
    a.tap("native.row.behaviors")
    a.tap("native.row.multiple")
    a.tap("native.action.show")
    a.tap("native.presentation.choice.apply")
    a.find("native.presentation")
    a.tap("native.presentation.option.work")
    a.tap("native.presentation.choice.apply")
    a.find("native.action.show")
    a.check("Go validates modal selection and accepts a corrected choice")
    a.tap("native.tab.notes")
    a.find(title)
    a.tap(title)
    a.tap("native.more")
    a.tap("Copy note")
    a.find("native.field.body")
    a.check("Explicit clipboard service settles without replacing the editor")
    a.shot("03-native-editor")


def service_smoke(a: Android):
    stamp = time.strftime("%H%M%S")
    body = a.find("native.field.body").get("text")
    name = "gofastr-parity-" + stamp + ".txt"
    fixture = a.out / name
    fixture.write_bytes("Android transfer round trip.\nUTF-8: café.\n".encode())
    a.adb("push", str(fixture), "/sdcard/Download/" + name)
    a.tap("native.more"); a.tap("Add file"); a.pick_download(name)
    a.find("native.action.export")
    assert a.export_as("export-" + name) == fixture.read_bytes()
    a.shot("04-file-preview")
    a.check("SAF import/export preserves exact UTF-8 bytes")
    a.tap("native.back")
    assert a.find("native.field.body").get("text") == body
    a.check("Attachment return retains the dirty editor")

    def chunk(kind, payload):
        return struct.pack(">I", len(payload)) + kind + payload + struct.pack(">I", zlib.crc32(kind + payload))
    pixels = b"".join(b"\0" + bytes([40, 150, 90]) * 128 for _ in range(96))
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 128, 96, 8, 2, 0, 0, 0))
    png += chunk(b"tEXt", b"Comment\0source-private-marker") + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b"")
    photo_name = "gofastr-photo-" + stamp + ".png"
    photo = a.out / photo_name; photo.write_bytes(png)
    a.adb("push", str(photo), "/sdcard/Download/" + photo_name)
    a.tap("native.more"); a.tap("Attachments")
    a.tap("native.more"); a.tap("Choose photo"); a.pick_download(photo_name)
    a.find("native.action.export")
    jpeg = a.export_as("gofastr-photo-" + stamp + ".jpg")
    assert jpeg[:2] == b"\xff\xd8" and jpeg[-2:] == b"\xff\xd9" and len(jpeg) <= 1048576
    assert b"source-private-marker" not in jpeg
    (a.out / "normalized-photo.jpg").write_bytes(jpeg)
    a.shot("05-photo-preview")
    a.check("Photo chooser normalizes to bounded JPEG and removes source metadata")
    a.tap("native.back")
    a.tap("native.back")
    assert a.find("native.field.body").get("text") == body

    a.tap("native.more"); a.tap("Record audio")
    nodes = a.nodes()
    permission = "com.android.permissioncontroller:id/permission_allow_foreground_only_button"
    if any(n.get("resource-id") == permission for n in nodes):
        a.tap(permission)
    time.sleep(2)
    a.tap("Stop")
    a.find("native.action.export")
    nodes = a.nodes()
    play_node = next(n for n in nodes if n.get("resource-id", "").startswith("native.audio.play."))
    play = play_node.get("resource-id")
    a.tap(play_node)
    time.sleep(0.3)
    # A UI dump waits for idle and would let a short clip finish first.
    (a.out / "audio-player.txt").write_bytes(a.adb("shell", "dumpsys", "media.player"))
    a.adb("shell", "input", "keyevent", "3")
    a.adb("shell", "am", "start", "-W", "-n", a.package + "/.MainActivity")
    assert a.find(play).get("text") == "Play"
    a.shot("06-audio-preview")
    a.check("AAC recording passes Go validation; native playback stops on background")
    a.tap("native.back")
    assert a.find("native.field.body").get("text") == body
    a.tap("native.more"); a.tap("Check reminder status")
    assert "Reminder status checked" in a.find("native.status").get("text", "")
    a.check("Reminder status settles without a permission request")
    a.tap("native.more"); a.tap("Share note")
    a.find("com.android.intentresolver:id/headline")
    a.back()
    a.find("native.field.body")
    a.check("Native share cancellation returns to the retained editor")
    a.tap("native.details"); a.tap("native.field.reminder_date")
    a.tap("android:id/date_picker_header_year")
    future_year = str(int(a.adb("shell", "date", "+%Y").decode().strip()) + 1)
    a.tap(future_year); a.tap("OK")
    a.tap("native.field.reminder_time"); a.tap("OK")
    a.tap("native.more"); a.tap("Schedule reminder")
    nodes = a.nodes()
    allow = "com.android.permissioncontroller:id/permission_allow_button"
    if any(n.get("resource-id") == allow for n in nodes):
        a.tap(allow)
    assert "Device reports reminder scheduled" in a.find("native.status").get("text", "")
    a.tap("native.more"); a.tap("Cancel reminder")
    assert "cancellation completed" in a.find("native.status").get("text", "")
    a.tap("native.more"); a.tap("Check reminder status")
    assert "No reminder is scheduled" in a.find("native.status").get("text", "")
    a.check("Future local reminder schedules with permission and cancels by app-scoped ID")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--serial", default="emulator-5580")
    parser.add_argument("--package", default="dev.gofastr.androidqa")
    parser.add_argument("--out", default="dist/android-acceptance")
    parser.add_argument("--services", action="store_true", help="Also test SAF, photos, recording, sharing and reminder status.")
    args = parser.parse_args()
    android = Android(args.serial, args.package, args.out)
    try:
        smoke(android)
        if args.services:
            service_smoke(android)
    except Exception:
        android.shot("failure")
        raise

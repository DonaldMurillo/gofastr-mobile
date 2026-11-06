"""Run the shared device-feature demo on an isolated emulator, without clearing data."""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import zipfile
import xml.etree.ElementTree as ET

from android_smoke import Android


def build_fixture(sdk: Path, out: Path) -> Path:
    tools = sorted((sdk / "build-tools").iterdir(), key=lambda p: tuple(int(v) for v in p.name.split(".")))[-1]
    platform = max((sdk / "platforms").glob("android-*"), key=lambda p: int(p.name.split("-")[-1])) / "android.jar"
    java = Path(os.environ["JAVA_HOME"]) / "bin"
    source = Path(__file__).parent / "android-feature-fixture"
    out.mkdir(parents=True, exist_ok=True)
    classes, dex = out / "classes", out / "dex"
    classes.mkdir(exist_ok=True); dex.mkdir(exist_ok=True)
    def run(*args): subprocess.run([str(a) for a in args], check=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
    run(java / "javac.exe", "--release", "11", "-classpath", platform, "-d", classes, source / "Fixture.java")
    run(java / "java.exe", "-cp", tools / "lib/d8.jar", "com.android.tools.r8.D8", "--min-api", "24", "--lib", platform, "--output", dex, *classes.rglob("*.class"))
    base = out / "base.apk"
    run(tools / "aapt2.exe", "link", "-o", base, "--manifest", source / "AndroidManifest.xml", "-I", platform)
    unsigned = out / "unsigned.apk"
    with zipfile.ZipFile(base) as origin, zipfile.ZipFile(unsigned, "w", compression=zipfile.ZIP_DEFLATED) as apk:
        for entry in origin.infolist(): apk.writestr(entry, origin.read(entry.filename))
        apk.write(dex / "classes.dex", "classes.dex")
    aligned, signed = out / "aligned.apk", out / "fixture.apk"
    run(tools / "zipalign.exe", "-f", "4", unsigned, aligned)
    run(java / "java.exe", "-jar", tools / "lib/apksigner.jar", "sign", "--ks", Path.home() / ".android/debug.keystore", "--ks-pass", "pass:android", "--key-pass", "pass:android", "--out", signed, aligned)
    return signed


def debug_demo(sdk: Path, original: Path, out: Path, package: str) -> Path:
    """Make a test-only debuggable copy. The delivered APK stays unchanged."""
    work = original.parent / "work/android"
    manifest = work / "AndroidManifest.xml"
    if ET.parse(manifest).getroot().get("package") != package: raise ValueError("APK manifest package mismatch")
    tools = sorted((sdk / "build-tools").iterdir(), key=lambda p: tuple(int(v) for v in p.name.split(".")))[-1]
    platform = max((sdk / "platforms").glob("android-*"), key=lambda p: int(p.name.split("-")[-1])) / "android.jar"
    java = Path(os.environ["JAVA_HOME"]) / "bin/java.exe"
    def run(*args): subprocess.run([str(a) for a in args], check=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
    base, unsigned, aligned, signed = [out / name for name in ("debug-base.apk", "debug-unsigned.apk", "debug-aligned.apk", "debug-demo.apk")]
    run(tools / "aapt2.exe", "link", "--debug-mode", "-o", base, "--manifest", manifest, "-I", platform, "-R", work / "widget-resources.zip")
    with zipfile.ZipFile(base) as resources, zipfile.ZipFile(original) as origin, zipfile.ZipFile(unsigned, "w", compression=zipfile.ZIP_DEFLATED) as apk:
        for entry in resources.infolist(): apk.writestr(entry, resources.read(entry.filename))
        for entry in origin.infolist():
            if entry.filename in resources.namelist() or entry.filename.startswith("META-INF/"): continue
            apk.writestr(entry, origin.read(entry.filename))
    run(tools / "zipalign.exe", "-f", "4", unsigned, aligned)
    run(java, "-jar", tools / "lib/apksigner.jar", "sign", "--ks", Path.home() / ".android/debug.keystore", "--ks-pass", "pass:android", "--key-pass", "pass:android", "--out", signed, aligned)
    return signed


class Features(Android):
    def action(self, label: str, primary: bool = False):
        if not primary: self.tap("native.more")
        self.tap(label)

    def status(self, contains: str):
        value = self.find("native.status").get("text", "")
        if contains not in value: raise AssertionError(f"Expected {contains!r}, got {value!r}")
        return value

    def start(self, route: str = ""):
        args = ["shell", "am", "start", "-W", "-n", self.package + "/.MainActivity"]
        if route: args += ["-a", "android.intent.action.VIEW", "-d", "gofastr-features:" + route]
        self.adb(*args)

    def fixture(self, mode: str, oversized: bool = False):
        args = ["shell", "am", "start", "-W", "-f", "0x10008000", "-n", "dev.gofastr.featurefixture/.Fixture", "--es", "target", self.package, "--es", "mode", mode]
        if oversized: args += ["--ez", "oversized", "true"]
        self.adb(*args)

    def inbox(self):
        values = self.adb("shell", "run-as", self.package, "ls", "files/gofastr-incoming-shares").decode().split()
        return [v for v in values if v.endswith(".json")]

    def biometrics(self):
        """Requires a virtual fingerprint already enrolled as emulator ID 1."""
        self.start("/features/secure")
        self.action("Check availability", True)
        self.status("Biometrics available: true")
        self.action("Store with biometrics")
        self.find("Read or save the demo secret")
        self.shot("biometric-prompt")
        self.adb("emu", "finger", "touch", "1")
        self.status("Secure operation completed")
        self.action("Read with biometrics")
        self.find("Read or save the demo secret")
        self.back(); self.status("Device result: cancelled")
        self.check("cancelled biometric read releases no secret bytes")
        self.action("Read with biometrics")
        self.find("Read or save the demo secret")
        self.adb("emu", "finger", "touch", "1")
        self.status("Read 19 secret bytes")
        self.check("virtual strong biometric authorizes Keystore encryption and decryption")
        self.action("Read with biometrics")
        self.find("Read or save the demo secret")
        self.adb("shell", "input", "keyevent", "3")
        self.start()
        self.status("Device result: cancelled")
        self.check("backgrounding cancels the biometric prompt without releasing bytes")
        self.action("Delete biometric secret")
        self.status("Secure operation completed")

    def run(self, fixture_apk: Path):
        self.adb("install", "-r", "--no-incremental", str(fixture_apk))
        self.start()
        self.find("native.row.secure")
        self.check("shared Go demo launches with all three opt-in features")
        self.tap("native.row.secure")
        self.action("Check availability", True); self.status("Secure storage available")
        self.action("Store demo secret"); self.status("Secure operation completed")
        name = hashlib.sha256(b"device:demo").hexdigest()
        location = "no_backup/gofastr-secure-v1/" + name
        encrypted = self.adb("shell", "run-as", self.package, "cat", location)
        if b"gofastr-demo-secret" in encrypted or base64.b64encode(b"gofastr-demo-secret") in encrypted: raise AssertionError("Secret stored without encryption")
        self.action("Read demo secret"); self.status("Read 19 secret bytes")
        self.check("Keystore write/read returns bytes while private file contains ciphertext")
        self.adb("shell", "am", "force-stop", self.package)
        self.start("/features/secure")
        self.action("Read demo secret"); self.status("Read 19 secret bytes")
        self.check("Keystore secret survives process restart")
        # Corrupt only this demo record in this isolated test app.
        self.adb("shell", "run-as", self.package, "sh", "-c", "'printf corrupt > " + location + "'")
        self.action("Read demo secret"); self.status("Device result: failed")
        self.check("tampered ciphertext fails without returning secret bytes")
        self.action("Delete demo secret"); self.status("Secure operation completed")
        self.action("Read demo secret"); self.status("No secret bytes returned")
        self.check("delete and missing reads settle without secret content")
        self.action("Authenticate")
        # A pristine emulator has no enrolled biometrics. Accept its observed
        # unavailable outcome; real enrolled success/cancel is a separate gate.
        nodes = self.nodes()
        if any("Read or save the demo secret" == n.get("text") for n in nodes): self.back()
        result = self.find("native.status").get("text", "")
        if not any(s in result for s in ("denied", "unsupported", "cancelled")): raise AssertionError(result)
        self.check("unavailable or cancelled biometric prompt does not report success")
        self.start("/features/inbox")
        self.adb("shell", "am", "start", "-W", "-n", self.package + "/.MainActivity", "-a", "android.intent.action.SEND", "-t", "text/plain", "--es", "android.intent.extra.TEXT", "share-in-smoke")
        self.action("Read next", True)
        self.status("share-in-smoke")
        first = self.inbox()
        self.action("Read next", True); self.status("share-in-smoke")
        if self.inbox() != first: raise AssertionError("Read consumed or duplicated share")
        self.action("Discard reviewed share"); self.status("discarded")
        self.action("Read next", True); self.status("empty")
        self.check("warm text share is stable across reads and removed by explicit acknowledgement")
        self.adb("shell", "am", "force-stop", self.package)
        self.fixture("send")
        self.find("native.row.inbox"); self.tap("native.row.inbox")
        self.action("Read next", True); self.status("Shared with a URI grant")
        shares = self.inbox()
        if len(shares) != 1: raise AssertionError(shares)
        payload = json.loads(self.adb("shell", "run-as", self.package, "cat", "files/gofastr-incoming-shares/" + shares[0]))
        file = payload["files"][0]
        if file["name"] != "fixture.txt" or base64.b64decode(file["data"]) != b"Shared provider bytes\n": raise AssertionError("Provider bytes changed")
        self.adb("shell", "am", "force-stop", self.package)
        self.start("/features/inbox")
        self.action("Read next", True); self.status(payload["identifier"])
        self.check("cold share copies granted provider bytes into a durable private inbox")
        self.shot("inbox")
        self.action("Discard reviewed share"); self.status("discarded")
        self.fixture("send", oversized=True)
        self.find("native.status")
        self.action("Read next", True); self.status("empty")
        self.check("oversized provider share is rejected without inbox publication")
        self.start("/features/widgets")
        self.action("Publish summary", True); self.status("published: true")
        self.fixture("widget")
        nodes = self.nodes()
        if any(n.get("text") == "Allow" for n in nodes): self.tap("Allow")
        elif any(n.get("text") == "Create" for n in nodes): self.tap("Create")
        self.find("Widget test host")
        self.find("GoFastr device features")
        self.find("Published from shared Go code\nAndroid and iOS: One Go snapshot")
        self.shot("widget")
        self.check("real AppWidgetHost renders published RemoteViews")
        self.tap("GoFastr device features")
        self.find("native.action.publish")
        self.check("widget tap opens the declared Go route")
        self.fixture("widget")
        self.find("GoFastr device features")
        root = next(n for n in self.nodes() if n.get("resource-id") == "android:id/content" and n.get("class") == "android.widget.LinearLayout")
        bounds = [int(v) for v in re.findall(r"\d+", root.get("bounds", ""))]
        self.adb("shell", "input", "tap", str((bounds[0] + bounds[2]) // 2), str(bounds[3] - 15))
        self.find("native.action.publish")
        self.check("widget padding also opens the declared Go route")
        self.action("Clear summary"); self.status("published: false")
        self.fixture("widget")
        self.find("Open the app to choose widget content.")
        self.check("clearing widget removes public summary from a live host")
        self.shot("widget-cleared")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--serial", default="emulator-5580")
    parser.add_argument("--package", default="dev.gofastr.featuresqa")
    parser.add_argument("--out", default="dist/android-feature-acceptance")
    parser.add_argument("--apk", default="dist/mobile/device-features/Featuresnative.apk")
    parser.add_argument("--biometrics-only", action="store_true", help="Use an already enrolled virtual fingerprint 1; no enrollment or lock settings are changed.")
    args = parser.parse_args()
    sdk = Path(os.environ.get("ANDROID_HOME", "C:/Android/android-sdk"))
    os.environ["PATH"] = str(sdk / "platform-tools") + os.pathsep + os.environ["PATH"]
    output = Path(args.out)
    if args.biometrics_only:
        Features(args.serial, args.package, args.out).biometrics()
        raise SystemExit(0)
    fixture = build_fixture(sdk, output / "fixture")
    debug = debug_demo(sdk, Path(args.apk), output, args.package)
    driver = Features(args.serial, args.package, args.out)
    driver.adb("install", "-r", "--no-incremental", str(debug))
    driver.run(fixture)

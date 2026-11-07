package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// inspectCanary is planted in every app-controlled string the fixtures
// produce: titles, text, labels, values, row content, concrete routes and
// queries, raw connection and instance IDs, error messages, panic values
// and app output. No inspect output may contain it.
const inspectCanary = "zqxcanary7f3a"

// The fixture speaks just enough of the protocol for the harness. It logs
// every command kind to INSPECT_FIXTURE_LOG and fails on any kind other
// than hello, open and close. INSPECT_FIXTURE_FAIL selects a failure:
// start, command or panic.
const inspectFixtureSource = `package fixture

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"
)

const canary = "` + inspectCanary + `"

var privacy bool

func noise() {
	fmt.Println("stdout " + canary)
	fmt.Fprintln(os.Stderr, "stderr "+canary)
	log.Print("log " + canary)
	if raw := os.NewFile(uintptr(syscall.Stdout), "raw-stdout"); raw != nil {
		fmt.Fprintln(raw, "raw "+canary)
	}
}

func Start(dataDir string) error {
	noise()
	if os.Getenv("INSPECT_FIXTURE_FAIL") == "start" {
		return fmt.Errorf("fixture start failed for %s in %s", canary, dataDir)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil || len(entries) != 0 {
		return fmt.Errorf("data directory not empty or missing: %v", err)
	}
	return nil
}

func Stop() error { noise(); return nil }

func Root() string { return "/" }

type row struct {
	ID          string ` + "`json:\"id\"`" + `
	Title       string ` + "`json:\"title\"`" + `
	Subtitle    string ` + "`json:\"subtitle,omitempty\"`" + `
	Destination string ` + "`json:\"destination\"`" + `
	Disabled    bool   ` + "`json:\"disabled,omitempty\"`" + `
}

const query = "/b?token=" + canary
const record = "/notes/" + canary + "-42"

func Command(payload string) (string, error) {
	var c struct {
		Kind, Route, Instance string
		Hello *struct{ Capabilities map[string]string }
	}
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		return "", err
	}
	if path := os.Getenv("INSPECT_FIXTURE_LOG"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintln(f, c.Kind)
			f.Close()
		}
	}
	reply := map[string]any{"version": 1, "connection": "conn-" + canary}
	switch c.Kind {
	case "hello":
		privacy = c.Hello.Capabilities["privacy/v1"] == "available"
	case "close":
	case "open":
		noise()
		if c.Route == query {
			switch os.Getenv("INSPECT_FIXTURE_FAIL") {
			case "command":
				return "", errors.New("fixture command failed: " + canary)
			case "panic":
				panic("fixture panic " + canary)
			}
		}
		reply["instance"] = "inst-" + canary + c.Route
		switch c.Route {
		case "/":
			reply["screen"] = map[string]any{"route": "/", "title": "Home " + canary, "prompt": canary,
				"actions": []map[string]string{{"id": canary, "label": canary}},
				"tabs":    []map[string]string{{"id": canary, "label": canary, "route": "/"}, {"id": "a", "label": canary, "route": "/a"}},
				"list": map[string]any{"searchKey": canary, "empty": canary, "rows": []row{
					{ID: canary, Title: "Row " + canary, Subtitle: canary, Destination: query},
					{ID: "locked", Title: canary, Destination: "/locked"},
					{ID: "off", Title: canary, Destination: "/off", Disabled: true},
					{ID: "weird", Title: canary, Destination: "/weird"},
				}}}
		case "/a":
			reply["screen"] = map[string]any{"route": "/a", "title": canary, "values": map[string]string{"name": canary},
				"form": map[string]any{"status": canary, "fields": []map[string]any{
					{"key": canary, "label": canary, "kind": "text", "value": canary, "required": true},
					{"key": "choice", "label": canary, "kind": "select", "value": "x", "options": []map[string]string{{"value": "x", "label": canary}}},
					{"key": "odd", "label": canary, "kind": canary},
				}}}
		case query:
			reply["screen"] = map[string]any{"route": query, "title": canary, "text": "Body " + canary,
				"list": map[string]any{"rows": []row{{ID: canary, Title: canary, Destination: record}}}}
		case record:
			reply["screen"] = map[string]any{"route": record, "title": canary, "text": canary,
				"requires": []map[string]string{{"capability": canary + "/v1"}}}
		case "/locked":
			if !privacy {
				delete(reply, "instance")
				reply["error"] = map[string]string{"code": "unsupported", "message": "nativeui: required capability \"privacy/v1\" is unavailable " + canary}
			} else {
				reply["screen"] = map[string]any{"route": "/locked", "title": canary, "text": canary,
					"requires": []map[string]string{{"capability": "privacy/v1"}}, "protection": map[string]string{"lockRoute": "/"}}
			}
		case "/weird":
			delete(reply, "instance")
			reply["error"] = map[string]string{"code": canary, "message": canary}
		default:
			return "", errors.New("fixture: unexpected route " + c.Route)
		}
	default:
		return "", errors.New("fixture: forbidden command kind " + c.Kind)
	}
	encoded, err := json.Marshal(reply)
	return string(encoded), err
}
`

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
}

// fixtureModule writes a standalone module with package fixture holding
// source, and moves the test into it.
func fixtureModule(t *testing.T, goMod, source string) (module, log, temp string) {
	t.Helper()
	requireGo(t)
	root := t.TempDir()
	module = filepath.Join(root, "module")
	temp = filepath.Join(root, "tmp")
	for _, dir := range []string{filepath.Join(module, "fixture"), temp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "fixture", "fixture.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	log = filepath.Join(root, "kinds.log")
	t.Setenv("INSPECT_FIXTURE_LOG", log)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod") // inspect must override this
	for _, name := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(name, temp)
	}
	t.Chdir(module)
	return module, log, temp
}

func protocolFixture(t *testing.T) (module, log, temp string) {
	return fixtureModule(t, "module example.com/inspectfixture\n\ngo 1.22\n", inspectFixtureSource)
}

func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(data)
		} else if path != dir {
			files[path+"/"] = ""
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func assertNoInspectLeftovers(t *testing.T, module, temp string, before map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(temp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "gofastr-inspect-") {
			t.Errorf("temporary directory left behind: %s", entry.Name())
		}
	}
	after := snapshotDir(t, module)
	if len(after) != len(before) {
		t.Errorf("module changed: before %d entries, after %d: %v", len(before), len(after), after)
	}
	for path, content := range before {
		if after[path] != content {
			t.Errorf("module file changed: %s", path)
		}
	}
}

// assertRedacted fails if any output carries app-controlled content, a raw
// protocol ID or a temporary path.
func assertRedacted(t *testing.T, temp string, outputs ...string) {
	t.Helper()
	for _, output := range outputs {
		for _, forbidden := range []string{inspectCanary, "conn-", "inst-", "gofastr-inspect-", temp, filepath.ToSlash(temp), "/notes/", "token=", "?"} {
			if strings.Contains(output, forbidden) {
				t.Errorf("output contains %q:\n%s", forbidden, output)
			}
		}
	}
}

func commandKinds(t *testing.T, log string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, kind := range strings.Fields(string(data)) {
		kinds[kind]++
	}
	for kind := range kinds {
		if kind != "hello" && kind != "open" && kind != "close" {
			t.Errorf("harness sent forbidden kind %q", kind)
		}
	}
	return kinds
}

func runInspect(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := inspectCommand(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func decodeReport(t *testing.T, stdout string) inspectReport {
	t.Helper()
	var report inspectReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("%v\n%s", err, stdout)
	}
	return report
}

func TestInspectWalksFixtureWithRedactedStructure(t *testing.T) {
	module, log, temp := protocolFixture(t)
	before := snapshotDir(t, module)
	code, stdout, stderr := runInspect(t, "--pkg", "./fixture", "--profile", "all", "--json")
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	assertRedacted(t, temp, stdout, stderr)
	report := decodeReport(t, stdout)
	if report.Synthetic != inspectSyntheticNote || report.RoutePatterns || report.Redaction != inspectOpaqueNote || len(report.Results) != 4 || report.Failure != nil {
		t.Fatalf("report: %+v", report)
	}
	byProfile := map[string]map[string]inspectScreen{}
	for _, result := range report.Results {
		if !result.Connected {
			t.Fatalf("profile %s did not connect: %+v", result.Profile, result)
		}
		screens := map[string]inspectScreen{}
		for _, screen := range result.Screens {
			if screen.Pattern != "" {
				t.Errorf("%s: pattern %q without a registry", result.Profile, screen.Pattern)
			}
			screens[screen.ID] = screen
		}
		byProfile[result.Profile] = screens
		// s1 /, s2 /a, s3 the query route, s4 /locked, s5 /weird, s6 the record.
		if len(screens) != 6 || screens["s6"].Depth != 2 || screens["s6"].Parent != "s3" || screens["s2"].Parent != "s1" {
			t.Errorf("%s walked %+v", result.Profile, result.Screens)
		}
		root := screens["s1"].Outline
		if root == nil || root.Content != "list" || root.Rows != 4 || root.DisabledRows != 1 || root.Tabs != 2 || root.Actions != 1 || !root.Search ||
			strings.Join(root.Links, ",") != "s1,s2,s3,s4,s5" || len(root.ActionIDs) != 0 {
			t.Errorf("%s root outline %+v", result.Profile, root)
		}
		form := screens["s2"].Outline
		if form == nil || form.Content != "form" || form.Fields["text"] != 1 || form.Fields["select"] != 1 || form.Fields["other"] != 1 || form.RequiredFields != 1 {
			t.Errorf("%s form outline %+v", result.Profile, form)
		}
		if text := screens["s3"].Outline; text == nil || text.Content != "list" || strings.Join(text.Links, ",") != "s6" {
			t.Errorf("%s query screen outline %+v", result.Profile, text)
		}
		if record := screens["s6"].Outline; record == nil || record.Content != "text" || len(record.Requires) != 0 {
			t.Errorf("%s record outline %+v (an undeclared capability must be dropped)", result.Profile, record)
		}
		if weird := screens["s5"]; weird.Status != "refused" || weird.Code != "other" {
			t.Errorf("%s unknown code = %+v", result.Profile, weird)
		}
	}
	// One screen keeps one ID in every profile, so refusals line up.
	if got := byProfile["webview"]["s4"]; got.Status != "refused" || got.Code != "unsupported" || got.Capability != "privacy/v1" || got.Outline != nil {
		t.Errorf("webview s4 = %+v", got)
	}
	if got := byProfile["uikit"]["s4"]; got.Status != "ok" || !got.Outline.Protected || len(got.Outline.Requires) != 1 || got.Outline.Requires[0].Capability != "privacy/v1" {
		t.Errorf("uikit s4 = %+v", got)
	}
	kinds := commandKinds(t, log)
	// 6 opens per profile; /weird never returns an instance and /locked
	// returns none under webview and minimal.
	if len(kinds) != 3 || kinds["hello"] != 4 || kinds["open"] != 24 || kinds["close"] != 18 {
		t.Errorf("command kinds = %v", kinds)
	}
	assertNoInspectLeftovers(t, module, temp, before)
}

func TestInspectTextReportIsRedacted(t *testing.T) {
	module, _, temp := protocolFixture(t)
	before := snapshotDir(t, module)
	code, stdout, stderr := runInspect(t, "--pkg", "./fixture", "--profile", "webview")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	assertRedacted(t, temp, stdout, stderr)
	for _, want := range []string{
		"ok       s1  depth 0  list rows=4 disabled=1 actions=1 tabs=2 search links=s1,s2,s3,s4,s5",
		"refused  s4  depth 1 from s1  unsupported, needs privacy/v1",
		"refused  s5  depth 1 from s1  other",
		"ok       s2  depth 1 from s1  form fields=other:1,select:1,text:1 required=1",
		"4 opened, 2 refused",
		inspectOpaqueNote,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	assertNoInspectLeftovers(t, module, temp, before)
}

func TestInspectDepthAndScreenLimits(t *testing.T) {
	protocolFixture(t)
	code, stdout, stderr := runInspect(t, "--pkg", "./fixture", "--profile", "uikit", "--depth", "1", "--max-screens", "3")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, " s6 ") || !strings.Contains(stdout, "3 opened, 0 refused, 2 not visited") || !strings.Contains(stdout, "assumed") {
		t.Errorf("output:\n%s", stdout)
	}
}

// Failures report a fixed phase and Go type names, never the app's error
// text, panic value or output.
func TestInspectFailuresAreRedactedAndCleanedUp(t *testing.T) {
	for _, test := range []struct {
		fail, phase, cause, screen string
		results                    int
	}{
		{"start", "start", "*fmt.wrapError", "", 0},
		{"command", "open", "*errors.errorString", "s3", 1},
		{"panic", "open", "panic", "s3", 1},
	} {
		t.Run(test.fail, func(t *testing.T) {
			module, log, temp := protocolFixture(t)
			before := snapshotDir(t, module)
			t.Setenv("INSPECT_FIXTURE_FAIL", test.fail)
			code, stdout, stderr := runInspect(t, "--pkg", "./fixture", "--json")
			if code != 1 {
				t.Fatalf("exit %d, want 1\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			assertRedacted(t, temp, stdout, stderr)
			report := decodeReport(t, stdout)
			failure := report.Failure
			if failure == nil || failure.Phase != test.phase || failure.Screen != test.screen || len(report.Results) != test.results {
				t.Fatalf("report: %+v failure %+v", report, failure)
			}
			if test.fail == "start" {
				// fmt.Errorf without %w is a *fmt.errorString in some Go
				// versions; accept any type name, but never the message.
				if failure.Cause == "" || strings.Contains(failure.Cause, " ") {
					t.Errorf("cause %q", failure.Cause)
				}
			} else if failure.Cause != test.cause || failure.Profile != "uikit" {
				t.Errorf("failure %+v", failure)
			}
			if !strings.Contains(stderr, "inspect: harness stopped: phase "+test.phase) {
				t.Errorf("stderr: %s", stderr)
			}
			if test.fail != "start" {
				commandKinds(t, log)
			}
			// Text output stays redacted too.
			_, text, textErr := runInspect(t, "--pkg", "./fixture")
			assertRedacted(t, temp, text, textErr)
			assertNoInspectLeftovers(t, module, temp, before)
		})
	}
}

// With GOFLAGS=-mod=mod, a plain go list adds the missing requirement to
// go.mod. inspect must refuse instead and leave go.mod and go.sum alone.
func TestInspectResolutionIsReadOnlyUnderModMod(t *testing.T) {
	const goMod = "module example.com/needsupdate\n\ngo 1.22\n\nreplace example.com/dep => ./dep\n"
	source := strings.Replace(inspectFixtureSource, "import (\n", "import (\n\t_ \"example.com/dep\"\n", 1)
	module, log, temp := fixtureModule(t, goMod, source)
	for name, content := range map[string]string{"dep/go.mod": "module example.com/dep\n\ngo 1.22\n", "dep/dep.go": "package dep\n"} {
		path := filepath.Join(module, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Prove the fixture needs a manifest update: plain go list rewrites a copy.
	control := filepath.Join(t.TempDir(), "control")
	if err := os.CopyFS(control, os.DirFS(module)); err != nil {
		t.Fatal(err)
	}
	list := exec.Command("go", "list", "./fixture")
	list.Dir = control
	if out, err := list.CombinedOutput(); err != nil {
		t.Fatalf("control go list: %v\n%s", err, out)
	}
	if changed, _ := os.ReadFile(filepath.Join(control, "go.mod")); string(changed) == goMod {
		t.Fatal("control: go list -mod=mod did not need to change go.mod; the fixture proves nothing")
	}

	before := snapshotDir(t, module)
	for _, args := range [][]string{{"--pkg", "./fixture"}, {"--pkg", "./fixture", "--dry-run"}} {
		code, stdout, stderr := runInspect(t, args...)
		if code != 1 || !strings.Contains(stderr, "-mod=readonly") {
			t.Errorf("%v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Error("app code ran although resolution failed")
	}
	if _, err := os.Stat(filepath.Join(module, "go.sum")); !os.IsNotExist(err) {
		t.Error("go.sum was created")
	}
	assertNoInspectLeftovers(t, module, temp, before)
}

func TestInspectDryRunWritesNothing(t *testing.T) {
	module, log, temp := protocolFixture(t)
	before := snapshotDir(t, module)
	code, stdout, stderr := runInspect(t, "--pkg", "./fixture", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run: nothing built") || !strings.Contains(stdout, "profile webview (renderer webview): ") || !strings.Contains(stdout, "not measurements from a device") {
		t.Errorf("output:\n%s", stdout)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Error("dry run ran app code")
	}
	assertNoInspectLeftovers(t, module, temp, before)
}

func TestInspectUsageErrors(t *testing.T) {
	module, _, _ := protocolFixture(t)
	for name, source := range map[string]string{
		"nocommand": "package nocommand\n\nfunc Start(string) error { return nil }\n",
		"badinspect": "package badinspect\n\nfunc Start(string) error { return nil }\nfunc Stop() error { return nil }\nfunc Root() string { return \"/\" }\n" +
			"func Command(string) (string, error) { return \"\", nil }\nfunc InspectApp() string { return \"\" }\n",
	} {
		if err := os.MkdirAll(filepath.Join(module, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(module, name, "x.go"), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{nil, "--pkg is required"},
		{[]string{"--pkg", "-x"}, "want an import path"},
		{[]string{"--pkg", "../escape"}, "want an import path"},
		{[]string{"--pkg", "./fixture", "--profile", "desktop"}, "--profile must be"},
		{[]string{"--pkg", "./fixture", "--route", "relative"}, "--route must start with /"},
		{[]string{"--pkg", "./fixture", "--depth", "-1"}, "--depth must be"},
		{[]string{"--pkg", "./nocommand"}, "inspect needs the versioned Command export"},
		{[]string{"--pkg", "./badinspect"}, "want func InspectApp() *nativeui.App"},
		{[]string{"--pkg", "./fixture", "extra"}, "unexpected arguments"},
	} {
		code, _, stderr := runInspect(t, test.args...)
		if code != 2 || !strings.Contains(stderr, test.want) {
			t.Errorf("%v: exit %d, stderr %q", test.args, code, stderr)
		}
	}
}

// A forged or malformed result file is discarded whole.
func TestInspectRejectsResultsOutsideTheContract(t *testing.T) {
	profiles := inspectProfiles()[:1]
	write := func(t *testing.T, value any) string {
		path := filepath.Join(t.TempDir(), "result.json")
		data, _ := json.Marshal(value)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	screen := func(edit func(map[string]any)) map[string]any {
		entry := map[string]any{"id": "s1", "depth": 0, "status": "ok", "outline": map[string]any{"content": "list"}}
		edit(entry)
		return map[string]any{"results": []any{map[string]any{"profile": "uikit", "renderer": "uikit", "connected": true, "screens": []any{entry}, "unvisited": 0}}}
	}
	valid := screen(func(map[string]any) {})
	if results, failure := inspectReadResult(write(t, valid), profiles, false); failure != nil || len(results) != 1 {
		t.Fatalf("valid result rejected: %+v", failure)
	}
	for name, forged := range map[string]any{
		"route id":    screen(func(entry map[string]any) { entry["id"] = "/notes/" + inspectCanary }),
		"pattern":     screen(func(entry map[string]any) { entry["pattern"] = "/notes/{id}" }),
		"title field": screen(func(entry map[string]any) { entry["title"] = inspectCanary }),
		"code": screen(func(entry map[string]any) {
			entry["status"], entry["code"], entry["outline"] = "refused", inspectCanary, nil
		}),
		"capability": screen(func(entry map[string]any) { entry["capability"] = inspectCanary + "/v1" }),
		"action ids": screen(func(entry map[string]any) {
			entry["outline"] = map[string]any{"content": "list", "actionIds": []string{"save"}}
		}),
		"field kind": screen(func(entry map[string]any) {
			entry["outline"] = map[string]any{"content": "form", "fields": map[string]int{inspectCanary: 1}}
		}),
		"link": screen(func(entry map[string]any) {
			entry["outline"] = map[string]any{"content": "list", "links": []string{"/b?x=1"}}
		}),
		"failure text": map[string]any{"results": []any{}, "failure": map[string]any{"phase": "start", "cause": "boom " + inspectCanary}},
	} {
		if results, failure := inspectReadResult(write(t, forged), profiles, false); failure == nil || failure.Phase != "result" || results != nil {
			t.Errorf("%s: accepted %+v %+v", name, results, failure)
		}
	}
	if _, failure := inspectReadResult(filepath.Join(t.TempDir(), "missing.json"), profiles, false); failure == nil || failure.Cause != "exited-without-result" {
		t.Errorf("missing result: %+v", failure)
	}
}

// registryFixtureSource is a real nativeui app with an InspectApp export
// behind the gofastr_inspect tag. Every string it renders holds the canary.
const registryFixtureSource = `package fixture

import (
	"context"
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

const canary = "` + inspectCanary + `"

var app *nativeui.App
var runtime *nativeui.Runtime

func handle(context.Context, nativeui.Request) (nativeui.Result, error) { return nativeui.Result{}, nil }

func Start(dataDir string) error {
	fmt.Println(canary, dataDir)
	fmt.Fprintln(os.Stderr, canary, dataDir)
	built, err := nativeui.New("/",
		nativeui.Route{Path: "/", Actions: map[string]nativeui.Handle{"archive": handle}, Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Route: request.Route, Title: "Home " + canary, List: &nativeui.List{Rows: []nativeui.Row{
				{ID: canary, Title: canary, Subtitle: canary, Destination: "/notes/" + canary + "?q=" + canary, TrailingActions: []nativeui.RowAction{{ID: "archive", Label: canary}}},
			}}}, nil
		}},
		nativeui.Route{Path: "/notes/{id}", Actions: map[string]nativeui.Handle{"save": handle}, Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Route: request.Route, Title: canary + request.Params["id"], Actions: []nativeui.Action{{ID: "save", Label: canary}},
				Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "body", Label: canary, Kind: "multiline", Value: canary}}}}, nil
		}},
	)
	if err != nil {
		return err
	}
	started, err := nativeui.NewRuntime(built, nativeui.RuntimeOptions{})
	if err != nil {
		return err
	}
	app, runtime = built, started
	return nil
}

func Stop() error { return nil }

func Root() string { return "/" }

func Command(payload string) (string, error) {
	return runtime.CommandJSON(context.Background(), payload)
}
`

const registryInspectSource = `//go:build gofastr_inspect

package fixture

import "github.com/DonaldMurillo/gofastr-native/nativeui"

func InspectApp() *nativeui.App { return app }
`

// registryGoMod points a fixture module at this checkout and copies its
// requirements, so -mod=readonly finds a complete graph.
func registryGoMod(t *testing.T, repo string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	requires := text[strings.Index(text, "require ("):]
	if tool := strings.Index(requires, "tool ("); tool >= 0 {
		requires = requires[:tool]
	}
	goLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "go ") {
			goLine = strings.TrimSpace(line)
		}
	}
	return "module example.com/registryfixture\n\n" + goLine + "\n\nrequire github.com/DonaldMurillo/gofastr-native v0.0.0\n\n" + requires +
		"\nreplace github.com/DonaldMurillo/gofastr-native => " + filepath.ToSlash(repo) + "\n"
}

func TestInspectRegistryAddsPatternsWithoutContent(t *testing.T) {
	requireGo(t)
	if testing.Short() {
		t.Skip("builds nativeui")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	module, _, temp := fixtureModule(t, registryGoMod(t, repo), registryFixtureSource)
	for name, content := range map[string]string{"go.sum": string(sum), "fixture/inspect_on.go": registryInspectSource} {
		if err := os.WriteFile(filepath.Join(module, filepath.FromSlash(name)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshotDir(t, module)
	code, stdout, stderr := runInspect(t, "--pkg", "./fixture", "--profile", "all", "--json")
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	// Registered patterns legitimately contain "/notes/"; check the rest.
	for _, output := range []string{stdout, stderr} {
		for _, forbidden := range []string{inspectCanary, "gofastr-inspect-", temp, filepath.ToSlash(temp), "?", "body"} {
			if strings.Contains(output, forbidden) {
				t.Errorf("output contains %q:\n%s", forbidden, output)
			}
		}
	}
	report := decodeReport(t, stdout)
	if !report.RoutePatterns || report.Redaction != inspectRegistryNote || len(report.Results) != 4 {
		t.Fatalf("report: %+v", report)
	}
	for _, result := range report.Results {
		if len(result.Screens) != 2 {
			t.Fatalf("%s: %+v", result.Profile, result.Screens)
		}
		root, note := result.Screens[0], result.Screens[1]
		if root.ID != "s1" || root.Pattern != "/" || strings.Join(root.Outline.RowActionIDs, ",") != "archive" ||
			strings.Join(root.Outline.Destinations, ",") != "/notes/{id}" || strings.Join(root.Outline.Links, ",") != "s2" {
			t.Errorf("%s root %+v %+v", result.Profile, root, root.Outline)
		}
		if note.ID != "s2" || note.Pattern != "/notes/{id}" || strings.Join(note.Outline.ActionIDs, ",") != "save" || note.Outline.Fields["multiline"] != 1 {
			t.Errorf("%s note %+v %+v", result.Profile, note, note.Outline)
		}
	}
	code, text, textErr := runInspect(t, "--pkg", "./fixture", "--profile", "uikit")
	if code != 0 || !strings.Contains(text, "ok       s2 /notes/{id}  depth 1 from s1  form fields=multiline:1 actions=1 action-ids=save") || strings.Contains(text+textErr, inspectCanary) {
		t.Errorf("exit %d text:\n%s\n%s", code, text, textErr)
	}
	assertNoInspectLeftovers(t, module, temp, before)
}

// TestInspectGalleryUnderSyntheticProfiles runs the real gallery through the
// harness: the webview profile must refuse the device-only screens. The
// gallery exports InspectApp under gofastr_inspect, so refusals carry
// registered patterns.
func TestInspectGalleryUnderSyntheticProfiles(t *testing.T) {
	requireGo(t)
	if testing.Short() {
		t.Skip("builds the gallery")
	}
	code, stdout, stderr := runInspect(t, "--pkg", "github.com/DonaldMurillo/gofastr-native/examples/gallery/gallerynative", "--profile", "webview", "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	report := decodeReport(t, stdout)
	if !report.RoutePatterns {
		t.Fatal("gallery InspectApp export not detected")
	}
	refused := map[string]string{}
	opened := 0
	for _, screen := range report.Results[0].Screens {
		if screen.Status == "refused" {
			refused[screen.Pattern] = screen.Capability
		} else {
			opened++
		}
	}
	for pattern, capability := range map[string]string{"/device/protected": "privacy/v1", "/device/clipboard": "clipboard/v1", "/device/share-text": "sharing/v1"} {
		if refused[pattern] != capability {
			t.Errorf("%s refused with %q, want %q (all refusals %v)", pattern, refused[pattern], capability, refused)
		}
	}
	if opened < 20 {
		t.Errorf("only %d gallery screens opened", opened)
	}
	if _, ok := refused["/feedback/sheet"]; ok {
		t.Error("sheet screen allows the fallback but was refused")
	}
}

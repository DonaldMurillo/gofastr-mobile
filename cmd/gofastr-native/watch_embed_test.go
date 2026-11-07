package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// embedFixture is a one-package module whose embed patterns cover a glob,
// a whole directory and an all: directory.
type embedFixture struct {
	t       *testing.T
	dir     string
	exclude []string
}

func newEmbedFixture(t *testing.T) *embedFixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &embedFixture{t: t, dir: dir, exclude: []string{filepath.Join(dir, "static", "out")}}
	f.write("go.mod", "module embedprobe\n\ngo 1.22\n")
	f.write("app.go", "package embedprobe\n\nimport \"embed\"\n\n"+
		"//go:embed assets/*.txt static \"all:web\"\nvar Assets embed.FS\n")
	f.write("assets/a.txt", "a")
	f.write("assets/b.txt", "b")
	f.write("static/s.css", "body{}")
	f.write("web/index.html", "<p>")
	return f
}

func (f *embedFixture) write(name, content string) {
	f.t.Helper()
	path := filepath.Join(f.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *embedFixture) remove(name string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(name))); err != nil {
		f.t.Fatal(err)
	}
}

func (f *embedFixture) set() watchSet {
	f.t.Helper()
	r := &runner{ctx: context.Background(), out: &bytes.Buffer{}, dir: f.dir}
	set, err := computeWatchSet(r, []watchQuery{{dir: f.dir, pattern: "."}}, f.exclude)
	if err != nil {
		f.t.Fatal(err)
	}
	return set
}

// changes reports what a poll sees after step, relative to before it.
func (f *embedFixture) changes(set watchSet, step func()) string {
	f.t.Helper()
	before := set.fingerprint()
	step()
	after := set.fingerprint()
	return strings.Join(changedPaths(before, after, 20), " ")
}

func (f *embedFixture) rel(name string) string {
	return filepath.Join(f.dir, filepath.FromSlash(name))
}

func TestWatchEmbedSeesAssetChangesWithoutARecompute(t *testing.T) {
	f := newEmbedFixture(t)
	set := f.set()
	if len(set.embeds) != 3 {
		t.Fatalf("embed trees: %+v", set.embeds)
	}

	cases := []struct {
		name string
		step func()
		want string // path that must change; empty means no change
	}{
		{"new matching file", func() { f.write("assets/c.txt", "c") }, "assets/c.txt"},
		{"removed file", func() { f.remove("assets/a.txt") }, "assets/a.txt (removed)"},
		{"new nested directory", func() { f.write("static/img/icons/x.svg", "<svg/>") }, "static/img/icons/x.svg"},
		{"edit in an existing asset", func() { f.write("static/s.css", "body{color:red}") }, "static/s.css"},
		{"hidden file under all:", func() { f.write("web/.well-known/x", "1") }, "web/.well-known/x"},
		{"unmatched file in an embed directory", func() { f.write("assets/notes.md", "x") }, ""},
		{"unmatched nested file", func() { f.write("assets/sub/d.txt", "d") }, ""},
		{"hidden file without all:", func() { f.write("static/.cache", "x") }, ""},
		{"underscore file without all:", func() { f.write("static/_draft.css", "x") }, ""},
		{"editor temporary", func() { f.write("static/s.css~", "x") }, ""},
		{"output directory", func() { f.write("static/out/app.apk", "x") }, ""},
		{"dist directory", func() { f.write("static/dist/bundle.js", "x") }, ""},
		{"file outside every embed root", func() { f.write("docs/readme.txt", "x") }, ""},
	}
	for _, c := range cases {
		got := f.changes(set, c.step)
		if c.want == "" {
			if got != "" {
				t.Errorf("%s: poll saw %s", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, f.rel(c.want)) {
			t.Errorf("%s: poll saw %q, want %s", c.name, got, c.want)
		}
	}
}

func TestWatchEmbedDoesNotFollowSymlinks(t *testing.T) {
	f := newEmbedFixture(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, f.rel("static/linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), f.rel("assets/link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	set := f.set()
	for path := range set.fingerprint() {
		if pathWithin(outside, path) || strings.Contains(path, "linked") || strings.HasSuffix(path, "link.txt") {
			t.Errorf("fingerprint followed a symlink: %s", path)
		}
	}
	got := f.changes(set, func() {
		if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outside, "new.txt"), []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if got != "" {
		t.Errorf("a change behind a symlink triggered: %s", got)
	}
}

// A refresh that only adds the new asset to the embedded file list neither
// swallows an asset added during the build nor triggers a build by itself.
func TestWatchEmbedRefreshKeepsAssetsAddedDuringTheBuild(t *testing.T) {
	f := newEmbedFixture(t)
	old := f.set()
	base := old.fingerprint() // build start
	f.write("assets/during.txt", "added while building")
	next := f.set() // refresh after the build
	if next.equal(old) {
		t.Fatal("the refreshed set does not list the new embedded file")
	}
	adjusted := rebase(base, old, next)
	changed := strings.Join(changedPaths(adjusted, next.fingerprint(), 10), " ")
	if !strings.Contains(changed, f.rel("assets/during.txt")) {
		t.Fatalf("asset added during the build was absorbed by the refresh: %q", changed)
	}

	// The follow-up build starts from the current state; its refresh finds
	// the same set, so nothing more is scheduled.
	base = next.fingerprint()
	again := f.set()
	if !again.equal(next) {
		t.Fatalf("an unchanged module produced a different watch set:\n%+v\n%+v", next, again)
	}
	if !sameFingerprint(rebase(base, next, again), again.fingerprint()) {
		t.Fatal("a refresh with no edits reads as a change")
	}
}

func TestEmbedTreeCovers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pkg")
	exclude := []string{filepath.Join(dir, "static", "out")}
	at := func(name string) string { return filepath.Join(dir, filepath.FromSlash(name)) }
	glob, _ := newEmbedTree(dir, "assets/*.txt")
	whole, _ := newEmbedTree(dir, "static")
	all, _ := newEmbedTree(dir, "all:web")
	for _, c := range []struct {
		tree embedTree
		path string
		want bool
	}{
		{glob, "assets/a.txt", true},
		{glob, "assets/.a.txt", true}, // named by the glob directly
		{glob, "assets/a.md", false},
		{glob, "assets/sub/a.txt", false},
		{whole, "static/a/b/c.css", true},
		{whole, "static/.hidden", false},
		{whole, "static/_x/c.css", false},
		{whole, "static/out/app.apk", false},
		{whole, "static/node_modules/x.js", false},
		{whole, "static/c.css.swp", false},
		{all, "web/.well-known/x", true},
		{all, "web/_next/x.js", true},
		{all, "web/a.swp", false},
		{whole, "other/c.css", false},
	} {
		if got := c.tree.covers(at(c.path), exclude); got != c.want {
			t.Errorf("%s covers %s = %v, want %v", c.tree.pattern, c.path, got, c.want)
		}
	}
	for _, bad := range []string{"", "all:", "../x", "a/../b", "/abs", "a/[", "./a"} {
		if _, ok := newEmbedTree(dir, bad); ok {
			t.Errorf("newEmbedTree accepted %q", bad)
		}
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// stamp is what a poll compares. A save by rename changes the set or the
// stamp, so editors that replace files are seen too.
type stamp struct {
	size int64
	mod  int64
	mode fs.FileMode
}

// watchSet is the polled input of one app. Directories are read
// non-recursively on every poll, so a new file in a watched package is
// seen without recomputing the set. Embed patterns are matched against the
// package directory on every poll, so a new file a pattern would embed,
// even one in a new subdirectory, is seen the same way.
type watchSet struct {
	dirs    []string    // package directories, sorted
	files   []string    // embedded files, go.mod and go.sum, sorted
	embeds  []embedTree // //go:embed patterns, sorted
	exclude []string    // the build output directory; never walked
}

// embedTree is one //go:embed pattern of one package. A poll reads only
// the directories the pattern can reach: each path element is matched with
// path.Match, and a matched directory is taken whole, as go:embed does.
// Symbolic links are never followed.
type embedTree struct {
	dir     string // package directory
	pattern string // slash-separated, without the all: prefix
	all     bool   // all: keeps names starting with . or _ inside matched directories
}

func (w watchSet) equal(other watchSet) bool {
	return slices.Equal(w.dirs, other.dirs) && slices.Equal(w.files, other.files) && slices.Equal(w.embeds, other.embeds)
}

// covers reports whether a poll of w would see path.
func (w watchSet) covers(path string) bool {
	if _, found := slices.BinarySearch(w.files, path); found {
		return true
	}
	if _, found := slices.BinarySearch(w.dirs, filepath.Dir(path)); found && !watchIgnoredName(filepath.Base(path)) {
		return true
	}
	for _, tree := range w.embeds {
		if tree.covers(path, w.exclude) {
			return true
		}
	}
	return false
}

func (w watchSet) fingerprint() map[string]stamp {
	stamps := map[string]stamp{}
	for _, dir := range w.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a removed directory reads as empty, which is a change
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() || watchIgnoredName(entry.Name()) {
				continue
			}
			if info, err := entry.Info(); err == nil {
				stamps[filepath.Join(dir, entry.Name())] = stamp{info.Size(), info.ModTime().UnixNano(), info.Mode()}
			}
		}
	}
	for _, path := range w.files {
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			stamps[path] = stamp{info.Size(), info.ModTime().UnixNano(), info.Mode()}
		}
	}
	for _, tree := range w.embeds {
		tree.walk(tree.dir, 0, false, w.exclude, stamps)
	}
	return stamps
}

func (t embedTree) elements() []string { return strings.Split(t.pattern, "/") }

// walk stamps the regular files under dir that t embeds. depth counts the
// pattern elements already matched. matched means an ancestor directory
// matched the whole pattern, so everything below it is embedded.
func (t embedTree) walk(dir string, depth int, matched bool, exclude []string, stamps map[string]stamp) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // a removed directory reads as empty, which is a change
	}
	elements := t.elements()
	for _, entry := range entries {
		name, child := entry.Name(), filepath.Join(dir, entry.Name())
		switch {
		case entry.IsDir():
			below, ok := t.enterDir(elements, depth, matched, name)
			if !ok || watchExcludedPath(child, exclude) || fileExists(filepath.Join(child, "go.mod")) {
				continue // go:embed never crosses into another module
			}
			t.walk(child, depth+1, below, exclude, stamps)
		case entry.Type().IsRegular():
			if !t.acceptFile(elements, depth, matched, name) {
				continue
			}
			if info, err := entry.Info(); err == nil {
				stamps[child] = stamp{info.Size(), info.ModTime().UnixNano(), info.Mode()}
			}
		}
		// Symbolic links and other irregular entries are skipped, so the
		// walk never leaves the package directory.
	}
}

// enterDir reports whether everything below a directory called name at
// depth is embedded, and whether the walk descends into it at all.
func (t embedTree) enterDir(elements []string, depth int, matched bool, name string) (below, ok bool) {
	if watchExcludedDirs[name] {
		return false, false
	}
	if matched {
		return true, t.all || !hiddenEmbedName(name)
	}
	if depth >= len(elements) {
		return false, false
	}
	if found, _ := path.Match(elements[depth], name); !found {
		return false, false
	}
	return depth+1 == len(elements), true
}

// acceptFile reports whether a regular file called name at depth is
// embedded. A file the pattern names directly is embedded even when hidden.
// Inside a matched directory, hidden names need all:.
func (t embedTree) acceptFile(elements []string, depth int, matched bool, name string) bool {
	if watchTemporaryName(name) {
		return false
	}
	if matched {
		return t.all || !hiddenEmbedName(name)
	}
	if depth+1 != len(elements) {
		return false
	}
	found, _ := path.Match(elements[depth], name)
	return found
}

// covers applies the walk's rules to one path, for rebase. It skips the
// nested-module check, which needs the file system.
func (t embedTree) covers(file string, exclude []string) bool {
	if watchExcludedPath(file, exclude) || !pathWithin(t.dir, file) {
		return false
	}
	rel, err := filepath.Rel(t.dir, file)
	if err != nil || rel == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	elements := t.elements()
	depth, matched := 0, false
	for _, name := range parts[:len(parts)-1] {
		var ok bool
		if matched, ok = t.enterDir(elements, depth, matched, name); !ok {
			return false
		}
		depth++
	}
	return t.acceptFile(elements, depth, matched, parts[len(parts)-1])
}

// go:embed leaves out names starting with . or _ inside a directory it
// embeds, unless the pattern starts with all:.
func hiddenEmbedName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func watchExcludedPath(path string, exclude []string) bool {
	for _, excluded := range exclude {
		if excluded != "" && pathWithin(excluded, path) {
			return true
		}
	}
	return false
}

func sameFingerprint(a, b map[string]stamp) bool {
	if len(a) != len(b) {
		return false
	}
	for path, s := range a {
		if other, ok := b[path]; !ok || other != s {
			return false
		}
	}
	return true
}

// changedPaths names up to limit differences, for the change message.
func changedPaths(before, after map[string]stamp, limit int) []string {
	var paths []string
	for path, s := range after {
		if old, ok := before[path]; !ok || old != s {
			paths = append(paths, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			paths = append(paths, path+" (removed)")
		}
	}
	sort.Strings(paths)
	if len(paths) > limit {
		paths = append(paths[:limit], fmt.Sprintf("and %d more", len(paths)-limit))
	}
	return paths
}

// Editor temporaries and hidden files never trigger a rebuild.
func watchIgnoredName(name string) bool {
	return strings.HasPrefix(name, ".") || watchTemporaryName(name)
}

// watchTemporaryName matches editor swap, backup and probe files. Hidden
// files that an all: embed pattern picks up still count.
func watchTemporaryName(name string) bool {
	switch {
	case strings.HasPrefix(name, "#"), strings.HasPrefix(name, ".#"), name == "4913":
		return true
	}
	for _, suffix := range []string{"~", ".swp", ".swx", ".swo", ".tmp", "___jb_tmp___", "___jb_old___"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

var watchExcludedDirs = map[string]bool{"dist": true, ".git": true, "node_modules": true, "vendor": true, "testdata": true}

type goListPackage struct {
	Dir        string
	ImportPath string
	Standard   bool
	Module     *goListModule

	EmbedPatterns, TestEmbedPatterns, XTestEmbedPatterns []string
	EmbedFiles, TestEmbedFiles, XTestEmbedFiles          []string
}

// watchQuery is one go list over a package pattern, run from dir.
type watchQuery struct {
	dir     string
	pattern string
	test    bool
}

// computeWatchSet asks the go tool for every package the app depends on and
// keeps those in the main module or a local replace, outside the module
// cache and outside exclude (the build output directory).
func computeWatchSet(r *runner, queries []watchQuery, exclude []string) (watchSet, error) {
	query := *r
	query.dryRun = false
	modCache, _ := query.quietOutput("go", "env", "GOMODCACHE")
	modCache = strings.TrimSpace(modCache)
	dirs, files, embeds := map[string]bool{}, map[string]bool{}, map[embedTree]bool{}
	for _, q := range queries {
		args := []string{"list", "-e", "-deps", "-json"}
		if q.test {
			args = append(args, "-test")
		}
		query.dir = q.dir
		cmd := query.command("go", append(args, q.pattern)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			return watchSet{}, fmt.Errorf("go list %s: %v\n%s", q.pattern, err, strings.TrimSpace(stderr.String()))
		}
		decoder := json.NewDecoder(&stdout)
		for {
			var p goListPackage
			if err := decoder.Decode(&p); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return watchSet{}, fmt.Errorf("go list %s: %w", q.pattern, err)
			}
			if p.Standard || p.Dir == "" || p.Module == nil || !watchLocalModule(p.Module) || pathWithin(modCache, p.Dir) {
				continue
			}
			if !watchIncludedDir(p.Module.Dir, p.Dir, exclude) {
				continue
			}
			dir := filepath.Clean(p.Dir)
			dirs[dir] = true
			for _, embedded := range slices.Concat(p.EmbedFiles, p.TestEmbedFiles, p.XTestEmbedFiles) {
				files[filepath.Join(dir, filepath.FromSlash(embedded))] = true
			}
			for _, pattern := range slices.Concat(p.EmbedPatterns, p.TestEmbedPatterns, p.XTestEmbedPatterns) {
				if tree, ok := newEmbedTree(dir, pattern); ok {
					embeds[tree] = true
				}
			}
			if p.Module.GoMod != "" {
				files[filepath.Clean(p.Module.GoMod)] = true
				files[filepath.Join(filepath.Dir(p.Module.GoMod), "go.sum")] = true
			}
		}
	}
	set := watchSet{exclude: exclude}
	for tree := range embeds {
		set.embeds = append(set.embeds, tree)
	}
	slices.SortFunc(set.embeds, func(a, b embedTree) int {
		if c := strings.Compare(a.dir, b.dir); c != 0 {
			return c
		}
		if c := strings.Compare(a.pattern, b.pattern); c != 0 {
			return c
		}
		return strings.Compare(fmt.Sprint(a.all), fmt.Sprint(b.all))
	})
	for dir := range dirs {
		set.dirs = append(set.dirs, dir)
	}
	for file := range files {
		set.files = append(set.files, file)
	}
	sort.Strings(set.dirs)
	sort.Strings(set.files)
	if len(set.dirs) == 0 {
		return set, errors.New("go list found no local packages to watch")
	}
	return set, nil
}

// newEmbedTree reads one go list EmbedPattern. go:embed rejects patterns
// with "." or ".." elements, so a tree never leaves dir.
func newEmbedTree(dir, pattern string) (embedTree, bool) {
	tree := embedTree{dir: dir, pattern: pattern}
	if rest, ok := strings.CutPrefix(pattern, "all:"); ok {
		tree.pattern, tree.all = rest, true
	}
	if tree.pattern == "" || path.Clean(tree.pattern) != tree.pattern || strings.HasPrefix(tree.pattern, "/") {
		return embedTree{}, false
	}
	for _, element := range tree.elements() {
		if element == "." || element == ".." {
			return embedTree{}, false
		}
		if _, err := path.Match(element, ""); err != nil {
			return embedTree{}, false
		}
	}
	return tree, true
}

func watchLocalModule(m *goListModule) bool {
	return m.Main || m.Replace != nil && m.Replace.Version == "" && m.Dir != ""
}

func watchIncludedDir(moduleDir, dir string, exclude []string) bool {
	for _, excluded := range exclude {
		if excluded != "" && pathWithin(excluded, dir) {
			return false
		}
	}
	rel, err := filepath.Rel(moduleDir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true // outside the module root (unusual); keep it
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if watchExcludedDirs[part] || part != "." && strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

// pathWithin reports whether path is base or inside it. Windows paths
// compare case-insensitively.
func pathWithin(base, path string) bool {
	if base == "" {
		return false
	}
	base, path = filepath.Clean(base), filepath.Clean(path)
	if runtime.GOOS == "windows" {
		base, path = strings.ToLower(base), strings.ToLower(path)
	}
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// rebase carries a build-start snapshot over to a new watch set. Paths the
// old set watched keep their build-start stamp, so an edit made during the
// build still counts. Paths only the new set watches take their current
// stamp, so a refreshed set alone never triggers a rebuild.
func rebase(base map[string]stamp, old, next watchSet) map[string]stamp {
	current := next.fingerprint()
	adjusted := map[string]stamp{}
	for path, s := range current {
		if old.covers(path) {
			if before, ok := base[path]; ok {
				adjusted[path] = before
			}
			continue // created during the build: absent, so it reads as a change
		}
		adjusted[path] = s
	}
	for path, before := range base {
		if _, ok := adjusted[path]; !ok && next.covers(path) {
			if _, exists := current[path]; !exists {
				adjusted[path] = before // removed during the build: reads as a change
			}
		}
	}
	return adjusted
}

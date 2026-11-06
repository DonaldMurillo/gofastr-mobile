package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// modernc's musl translation uses legacy Linux path syscalls on amd64.
// Android's app seccomp policy kills those calls, instead of returning ENOSYS.
// The build copy routes them to equivalent *at calls, without touching the module
// cache or changing Linux/iOS/Windows builds. Remove when libc supports Android.
func androidLibcOverlay(r *runner, work string) (string, error) {
	path := filepath.Join(work, "android-libc", "build")
	if r.dryRun {
		r.header("prepare Android SQLite syscall compatibility build")
		return path, nil
	}
	graph, err := r.outputBytes("read the pinned Android module graph", nil, "go", "list", "-m", "-json", "all")
	if err != nil {
		return "", err
	}
	type module struct {
		Path, Version, Dir, GoMod string
		Main                      bool
		Replace                   *struct{ Path, Version, Dir string }
	}
	decoder := json.NewDecoder(bytes.NewReader(graph))
	var modules []module
	dir := ""
	frameworkDir := ""
	for {
		var m module
		err := decoder.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		modules = append(modules, m)
		if m.Path == "modernc.org/libc" {
			dir = m.Dir
		}
		if m.Path == "github.com/DonaldMurillo/gofastr" {
			frameworkDir = m.Dir
		}
	}
	if dir == "" {
		return "", nil
	}
	original := filepath.Join(dir, "syscall_musl.go")
	content, err := os.ReadFile(original)
	if err != nil {
		return "", fmt.Errorf("Android SQLite compatibility: %w", err)
	}
	patched, err := patchAndroidLibc(content)
	if err != nil {
		return "", err
	}
	local, err := androidModuleSnapshot(work, dir, "module")
	if err != nil {
		return "", err
	}
	if err := r.do("copy SQLite libc into the Android build directory", func() error {
		return filepath.WalkDir(dir, func(source string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(dir, source)
			if err != nil {
				return err
			}
			destination := filepath.Join(local, relative)
			if entry.IsDir() {
				return os.MkdirAll(destination, 0700)
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("unexpected symlink in SQLite libc: %s", relative)
			}
			if fileExists(destination) {
				return nil
			}
			content, err := os.ReadFile(source)
			if err != nil {
				return err
			}
			return writeOwnerOnly(destination, content)
		})
	}); err != nil {
		return "", err
	}
	if err := writeSource(filepath.Join(local, "syscall_musl.go"), patched); err != nil {
		return "", err
	}
	frameworkLocal := ""
	if frameworkDir != "" {
		frameworkLocal, err = androidModuleSnapshot(work, frameworkDir, "gofastr")
		if err != nil {
			return "", err
		}
		if err := r.do("copy GoFastr for Android atomic identity publication", func() error {
			return filepath.WalkDir(frameworkDir, func(source string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				relative, err := filepath.Rel(frameworkDir, source)
				if err != nil {
					return err
				}
				destination := filepath.Join(frameworkLocal, relative)
				if entry.IsDir() {
					if relative != "." && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "dist") {
						return filepath.SkipDir
					}
					return os.MkdirAll(destination, 0700)
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("unexpected framework symlink: %s", relative)
				}
				if fileExists(destination) {
					return nil
				}
				content, err := os.ReadFile(source)
				if err != nil {
					return err
				}
				return writeOwnerOnly(destination, content)
			})
		}); err != nil {
			return "", err
		}
		ownerPath := filepath.Join(frameworkDir, "internal", "fileperm", "ownerwrite.go")
		ownerSource, err := os.ReadFile(ownerPath)
		if err != nil {
			return "", err
		}
		old := []byte("return os.Link(tmp.Name(), path)")
		if bytes.Count(ownerSource, old) != 1 {
			return "", fmt.Errorf("GoFastr identity publication source changed; review the Android compatibility copy")
		}
		ownerSource = bytes.Replace(ownerSource, old, []byte("return publishAndroidIdentity(tmp.Name(), path)"), 1)
		if err := writeSource(filepath.Join(frameworkLocal, "internal", "fileperm", "ownerwrite.go"), ownerSource); err != nil {
			return "", err
		}
		if err := writeSource(filepath.Join(frameworkLocal, "internal", "fileperm", "publish_android.go"), []byte(androidIdentityPublication)); err != nil {
			return "", err
		}
	}
	// A separate module preserves the application's dependency pins and local
	// replacements. gomobile copies this graph to its generated bind modules.
	m := new(modfile.File)
	if err := m.AddModuleStmt("gofastr.android.build"); err != nil {
		return "", err
	}
	if err := m.AddGoStmt("1.27.3"); err != nil {
		return "", err
	}
	for _, dependency := range modules {
		version := dependency.Version
		if version == "" {
			version = "v0.0.0"
		}
		if err := m.AddRequire(dependency.Path, version); err != nil {
			return "", err
		}
		if dependency.Path == "modernc.org/libc" {
			if err := m.AddReplace(dependency.Path, "", filepath.ToSlash(local), ""); err != nil {
				return "", err
			}
		}
		if dependency.Path == "github.com/DonaldMurillo/gofastr" && frameworkLocal != "" {
			if err := m.AddReplace(dependency.Path, "", filepath.ToSlash(frameworkLocal), ""); err != nil {
				return "", err
			}
		} else if dependency.Main {
			if err := m.AddReplace(dependency.Path, "", filepath.ToSlash(dependency.Dir), ""); err != nil {
				return "", err
			}
		}
		if dependency.Replace != nil && dependency.Path != "modernc.org/libc" && dependency.Path != "github.com/DonaldMurillo/gofastr" {
			replacement := dependency.Replace
			location := replacement.Path
			if replacement.Version == "" {
				location = filepath.ToSlash(replacement.Dir)
			}
			if err := m.AddReplace(dependency.Path, "", location, replacement.Version); err != nil {
				return "", err
			}
		}
	}
	encoded, err := m.Format()
	if err != nil {
		return "", err
	}
	if err := writeSource(filepath.Join(path, "go.mod"), encoded); err != nil {
		return "", err
	}
	for _, dependency := range modules {
		if dependency.Main && dependency.GoMod != "" {
			if sum, err := os.ReadFile(filepath.Join(filepath.Dir(dependency.GoMod), "go.sum")); err == nil {
				if err := writeSource(filepath.Join(path, "go.sum"), sum); err != nil {
					return "", err
				}
				break
			}
		}
	}
	probe := *r
	probe.dir = path
	probe.env = append(append([]string(nil), r.env...), "GOWORK=off")
	if err := probe.run("resolve checksums in the isolated Android module", "go", "mod", "download"); err != nil {
		return "", err
	}
	r.step = probe.step
	return path, nil
}

// Keep build copies separate when a pin or mutable local replacement changes.
// Hash source paths, sizes and modification times before copying. Old snapshots
// remain ordinary disposable build output; no user directory is removed.
func androidModuleSnapshot(work, source, label string) (string, error) {
	digest := sha256.New()
	fmt.Fprintln(digest, source)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative != "." && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(digest, "%s %d %d\n", relative, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return filepath.Join(work, "android-libc", fmt.Sprintf("%s-%x", label, digest.Sum(nil)[:8])), nil
}

func patchAndroidLibc(content []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "syscall_musl.go", content, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	count := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "unix" {
			return true
		}
		if selector.Sel.Name != "Syscall" && selector.Sel.Name != "Syscall6" {
			return true
		}
		call.Fun = ast.NewIdent("gofastrAndroidSyscall")
		for len(call.Args) < 7 {
			call.Args = append(call.Args, &ast.BasicLit{Kind: token.INT, Value: "0"})
		}
		count++
		return true
	})
	if count != 8 {
		return nil, fmt.Errorf("SQLite libc syscall source changed: got %d entry points, expected 8; review the Android compatibility overlay", count)
	}
	var output bytes.Buffer
	if err := format.Node(&output, fset, file); err != nil {
		return nil, err
	}
	output.WriteString(androidLibcSyscall)
	return format.Source(output.Bytes())
}

const androidLibcSyscall = `

// gofastr-native Android-only adaptation of legacy x86-64 path syscalls.
func gofastrAndroidSyscall(n,a,b,c,d,e,f uintptr)(uintptr,uintptr,unix.Errno) {
    if runtime.GOOS == "android" && runtime.GOARCH == "amd64" {
        cwd := ^uintptr(99) // AT_FDCWD, -100
        switch n {
        case 2: n,a,b,c,d = 257,cwd,a,b,c // open -> openat
        case 4: n,a,b,c,d = 262,cwd,a,b,0 // stat -> newfstatat
        case 6: n,a,b,c,d = 262,cwd,a,b,256 // lstat -> newfstatat, NOFOLLOW
        case 21: n,a,b,c,d = 269,cwd,a,b,0 // access -> faccessat
        case 33: n,d = 292,0; c=0 // dup2 -> dup3
        case 82: n,a,b,c,d = 264,cwd,a,cwd,b // rename -> renameat
        case 83: n,a,b,c = 258,cwd,a,b // mkdir -> mkdirat
        case 84: n,a,b,c = 263,cwd,a,512 // rmdir -> unlinkat, REMOVEDIR
        case 86: n,a,b,c,d,e = 265,cwd,a,cwd,b,0 // link -> linkat
        case 87: n,a,b,c = 263,cwd,a,0 // unlink -> unlinkat
        case 88: n,b,c = 266,cwd,b // symlink -> symlinkat
        case 89: n,a,b,c,d = 267,cwd,a,b,c // readlink -> readlinkat
        case 90: n,a,b,c = 268,cwd,a,b // chmod -> fchmodat
        case 92: n,a,b,c,d,e = 260,cwd,a,b,c,0 // chown -> fchownat
        case 94: n,a,b,c,d,e = 260,cwd,a,b,c,256 // lchown -> fchownat
        case 133: n,a,b,c,d = 259,cwd,a,b,c // mknod -> mknodat
        }
    }
    return unix.Syscall6(n,a,b,c,d,e,f)
}
`

// Android forbids hard links in app data. RENAME_NOREPLACE keeps the same
// synced, atomic, create-if-absent guarantee as the upstream hard-link publish.
const androidIdentityPublication = `package fileperm
import "golang.org/x/sys/unix"
func publishAndroidIdentity(from,to string) error {
    return unix.Renameat2(unix.AT_FDCWD,from,unix.AT_FDCWD,to,unix.RENAME_NOREPLACE)
}
`

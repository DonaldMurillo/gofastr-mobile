package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAndroidSQLiteOverlayIsLocalAndRejectsSourceDrift(t *testing.T) {
	if _, err := patchAndroidLibc([]byte("package libc\n")); err == nil {
		t.Fatal("unsupported dependency source was silently accepted")
	}
	directory := t.TempDir()
	path, err := androidLibcOverlay(&runner{ctx: context.Background(), out: io.Discard}, directory)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, directory) {
		t.Fatal("overlay escaped the build directory")
	}
	modules, err := filepath.Glob(filepath.Join(directory, "android-libc", "module-*"))
	if err != nil || len(modules) != 1 {
		t.Fatal(modules, err)
	}
	content, err := os.ReadFile(filepath.Join(modules[0], "syscall_musl.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "gofastrAndroidSyscall(") != 9 {
		t.Fatal("not all syscall entry points were adapted")
	}
}

func TestAndroidLegacySyscallsUseEquivalentAtArguments(t *testing.T) {
	// Execute the compatibility mapping on the host without issuing real syscalls.
	helper := strings.ReplaceAll(androidLibcSyscall, `runtime.GOOS == "android" && runtime.GOARCH == "amd64"`, `true`)
	helper = strings.ReplaceAll(helper, "unix.Errno", "syscall.Errno")
	helper = strings.ReplaceAll(helper, "unix.Syscall6", "record")
	program := `package main
import("fmt";"syscall")
var got [7]uintptr
func record(n,a,b,c,d,e,f uintptr)(uintptr,uintptr,syscall.Errno){got=[7]uintptr{n,a,b,c,d,e,f};return 0,0,0}
func main(){
cwd:=^uintptr(99)
tests:=[]struct{in [7]uintptr;want [7]uintptr}{
{[7]uintptr{6,11,22},[7]uintptr{262,cwd,11,22,256}},
{[7]uintptr{4,11,22},[7]uintptr{262,cwd,11,22,0}},
{[7]uintptr{2,11,22,33},[7]uintptr{257,cwd,11,22,33}},
{[7]uintptr{82,11,22},[7]uintptr{264,cwd,11,cwd,22}},
{[7]uintptr{87,11},[7]uintptr{263,cwd,11,0}},
{[7]uintptr{84,11},[7]uintptr{263,cwd,11,512}},
{[7]uintptr{90,11,22},[7]uintptr{268,cwd,11,22}},
{[7]uintptr{89,11,22,33},[7]uintptr{267,cwd,11,22,33}},
{[7]uintptr{1,11,22,33},[7]uintptr{1,11,22,33}},
}
for _,test:=range tests{a:=test.in;gofastrAndroidSyscall(a[0],a[1],a[2],a[3],a[4],a[5],a[6]);if got!=test.want{panic(fmt.Sprintf("%v: got %v want %v",a,got,test.want))}}
}
` + helper
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("go", "run", path).CombinedOutput(); err != nil {
		t.Fatalf("Android syscall mapping: %v %s", err, output)
	}
}

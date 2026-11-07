package clipboard

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSniff(t *testing.T) {
	for _, test := range []struct{ name, data, mime string }{
		{"png", "\x89PNG\r\n\x1a\n", "image/png"},
		{"jpeg", "\xff\xd8\xff\xe0", "image/jpeg"},
		{"gif87", "GIF87a", "image/gif"},
		{"gif89", "GIF89a", "image/gif"},
		{"webp", "RIFF1234WEBP", "image/webp"},
		{"bmp", "BM", "image/bmp"},
		{"empty", "", ""},
		{"text", "not an image", ""},
		{"short png", "\x89PNG", ""},
		{"short jpeg", "\xff\xd8", ""},
		{"short gif", "GIF89", ""},
		{"short riff", "RIFF1234WEB", ""},
		{"other riff", "RIFF1234WAVE", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			mime, ok := Sniff([]byte(test.data))
			if mime != test.mime || ok != (test.mime != "") {
				t.Fatalf("Sniff = %q, %v; want %q", mime, ok, test.mime)
			}
		})
	}
}

func TestImagePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dropped image.PNG")
	if err := os.WriteFile(path, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	for _, test := range []struct {
		name, paste string
		ok          bool
	}{
		{"plain", path, true},
		{"whitespace", " \t" + path + "\r\n", true},
		{"single quotes", "'" + path + "'", true},
		{"double quotes", `"` + path + `"`, true},
		{"escaped spaces", strings.ReplaceAll(path, " ", `\ `), true},
		{"url", fileURL, true},
		{"localhost url", strings.Replace(fileURL, "file:///", "file://localhost/", 1), true},
		{"home", "~/dropped image.PNG", true},
		{"missing", filepath.Join(dir, "missing.png"), false},
		{"multiple", path + "\n" + path, false},
		{"multiple quoted", `"` + path + `" "` + path + `"`, false},
		{"remote url", "file://remote/image.png", false},
		{"invalid url", "file:///%xx.png", false},
		{"url query", fileURL + "?image=1", false},
		{"url fragment", fileURL + "#image", false},
		{"nul", path + "\x00", false},
		{"empty", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ImagePath(test.paste)
			if ok != test.ok || ok && got != path {
				t.Fatalf("ImagePath(%q) = %q, %v; want %q, %v", test.paste, got, ok, path, test.ok)
			}
		})
	}
	for _, extension := range []string{"png", "jpg", "jpeg", "gif", "webp", "bmp", "JpEg", "txt"} {
		name := filepath.Join(dir, "file."+extension)
		if err := os.WriteFile(name, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if _, ok := ImagePath(name); ok != (extension != "txt") {
			t.Errorf("extension %q accepted = %v", extension, ok)
		}
	}
	directory := filepath.Join(dir, "directory.png")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if _, ok := ImagePath(directory); ok {
		t.Fatal("accepted a directory")
	}
}

func TestReadImageMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := ReadImage(context.Background())
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("missing tool: %v", err)
	}
}

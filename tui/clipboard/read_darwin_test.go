//go:build darwin

package clipboard

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func appleScript(t *testing.T, script string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-s", "s", "-")
	cmd.Stdin = strings.NewReader(script)
	return cmd.CombinedOutput()
}

// TestReadImageMacClipboard replaces the system clipboard while it runs, so
// it only runs when TUI_CLIPBOARD_TEST=1.
func TestReadImageMacClipboard(t *testing.T) {
	if os.Getenv("TUI_CLIPBOARD_TEST") != "1" {
		t.Skip("set TUI_CLIPBOARD_TEST=1 to test against the real clipboard")
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("osascript is unavailable")
	}
	saved, err := appleScript(t, "the clipboard as record")
	if err != nil {
		t.Skipf("no pasteboard session: %v: %s", err, saved)
	}
	if output, err := appleScript(t, "set the clipboard to "+strings.TrimSpace(string(saved))); err != nil {
		t.Skipf("cannot safely restore pasteboard: %v: %s", err, output)
	}
	t.Cleanup(func() {
		output, err := appleScript(t, "set the clipboard to "+strings.TrimSpace(string(saved)))
		if err != nil {
			t.Errorf("restore original clipboard: %v: %s", err, output)
		}
	})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "clipboard.png")
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("image", func(t *testing.T) {
		output, err := appleScript(t, "set the clipboard to (read (POSIX file "+strconv.Quote(path)+") as «class PNGf»)")
		if err != nil {
			t.Skipf("cannot write pasteboard: %v: %s", err, output)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		data, mime, err := ReadImage(ctx)
		if err != nil || mime != "image/png" {
			t.Fatalf("ReadImage = %q, %v", mime, err)
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil || decoded.Bounds() != image.Rect(0, 0, 3, 2) {
			t.Fatalf("decoded image size differs: %v", err)
		}
	})
	t.Run("text", func(t *testing.T) {
		output, err := appleScript(t, `set the clipboard to "text"`)
		if err != nil {
			t.Skipf("cannot write pasteboard: %v: %s", err, output)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _, err = ReadImage(ctx)
		if !errors.Is(err, ErrNoImage) {
			t.Fatalf("text clipboard: %v", err)
		}
	})
}

func TestReadImageMacToolResponses(t *testing.T) {
	for _, test := range []struct{ name, output string }{
		{"png", "«data PNGf" + hex.EncodeToString([]byte("\x89PNG\r\n\x1a\n")) + "»"},
		{"no image", "NO_IMAGE"},
		{"bad hex", "«data PNGfzz»"},
		{"unexpected", "text"},
		{"not image", "«data PNGf0102»"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte("#!/bin/sh\nprintf '%s\\n' '"+test.output+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			_, mime, err := ReadImage(context.Background())
			if test.name == "png" {
				if err != nil || mime != "image/png" {
					t.Fatalf("PNG response: %q, %v", mime, err)
				}
			} else if err == nil {
				t.Fatal("invalid response accepted")
			} else if (test.name == "no image" || test.name == "not image") && !errors.Is(err, ErrNoImage) {
				t.Fatalf("want ErrNoImage, got %v", err)
			}
		})
	}
}

func TestReadImageContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := ReadImage(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: %v", err)
	}
}

func TestReadImageTimeout(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte("#!/bin/sh\nexec /bin/sleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err := ReadImage(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}

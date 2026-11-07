// Package clipboard reads system clipboard images and dropped image paths.
package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNoImage means the clipboard does not contain a supported image.
var ErrNoImage = errors.New("clipboard has no image")

// ErrUnsupported means no clipboard tool is available on this system.
var ErrUnsupported = errors.New("clipboard image reading is unsupported")

// ReadImage reads an image from the system clipboard using an OS tool.
func ReadImage(ctx context.Context) (data []byte, mime string, err error) {
	data, err = readImage(ctx)
	if err != nil {
		return nil, "", err
	}
	mime, ok := Sniff(data)
	if !ok {
		return nil, "", ErrNoImage
	}
	return data, mime, nil
}

func command(ctx context.Context, tool string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(tool)
	if err != nil {
		return nil, ErrUnsupported
	}
	output, err := exec.CommandContext(ctx, path, args...).Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", tool, err)
	}
	return output, nil
}

// ImagePath recognizes one existing regular image file in pasted text.
func ImagePath(paste string) (path string, ok bool) {
	path = strings.TrimSpace(paste)
	if len(path) >= 2 && (path[0] == '\'' && path[len(path)-1] == '\'' || path[0] == '"' && path[len(path)-1] == '"') {
		path = path[1 : len(path)-1]
	}
	if strings.ContainsAny(path, "\r\n\x00") {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(path), "file://") {
		parsed, err := url.Parse(path)
		if err != nil || parsed.Host != "" && parsed.Host != "localhost" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", false
		}
		path = filepath.FromSlash(parsed.Path)
		if filepath.Separator == '\\' && len(path) >= 3 && path[0] == '\\' && path[2] == ':' {
			path = path[1:]
		}
	} else {
		path = strings.ReplaceAll(path, `\ `, " ")
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		path = filepath.Join(home, path[1:])
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
	default:
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

// Sniff recognizes PNG, JPEG, GIF, WebP and BMP magic bytes.
func Sniff(data []byte) (mime string, ok bool) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return "image/jpeg", true
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif", true
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp", true
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp", true
	}
	return "", false
}

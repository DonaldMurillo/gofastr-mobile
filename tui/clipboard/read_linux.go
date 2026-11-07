//go:build linux

package clipboard

import (
	"context"
	"errors"
	"os"
)

func readImage(ctx context.Context) ([]byte, error) {
	tool, args := "xclip", []string{"-selection", "clipboard", "-t", "image/png", "-o"}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		tool, args = "wl-paste", []string{"--type", "image/png"}
	}
	data, err := command(ctx, tool, args...)
	if err != nil && ctx.Err() == nil && !errors.Is(err, ErrUnsupported) {
		return nil, errors.Join(ErrNoImage, err)
	}
	return data, err
}

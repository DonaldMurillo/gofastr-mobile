//go:build darwin

package clipboard

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
)

func readImage(ctx context.Context) ([]byte, error) {
	output, err := command(ctx, "osascript", "-e", `try
return the clipboard as «class PNGf»
on error number -1700
return "NO_IMAGE"
end try`)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(output))
	if text == "NO_IMAGE" {
		return nil, ErrNoImage
	}
	if !strings.HasPrefix(text, "«data PNGf") || !strings.HasSuffix(text, "»") {
		return nil, fmt.Errorf("osascript: unexpected image response")
	}
	data, err := hex.DecodeString(strings.TrimSuffix(strings.TrimPrefix(text, "«data PNGf"), "»"))
	if err != nil {
		return nil, fmt.Errorf("osascript: decode PNG: %w", err)
	}
	return data, nil
}

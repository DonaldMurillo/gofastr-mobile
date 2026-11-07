//go:build windows

package clipboard

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
)

func readImage(ctx context.Context) ([]byte, error) {
	const script = `$ErrorActionPreference = 'Stop'; $image = Get-Clipboard -Format Image; if ($null -eq $image) { Write-Output 'NO_IMAGE'; exit }; $stream = New-Object System.IO.MemoryStream; try { $image.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png); [Convert]::ToBase64String($stream.ToArray()) } finally { $stream.Dispose(); $image.Dispose() }`
	output, err := command(ctx, "powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	if errors.Is(err, ErrUnsupported) {
		output, err = command(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(output))
	if text == "NO_IMAGE" {
		return nil, ErrNoImage
	}
	return base64.StdEncoding.DecodeString(text)
}

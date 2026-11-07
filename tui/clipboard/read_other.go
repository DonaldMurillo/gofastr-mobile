//go:build !darwin && !linux && !windows

package clipboard

import "context"

func readImage(context.Context) ([]byte, error) { return nil, ErrUnsupported }

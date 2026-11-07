//go:build !(darwin || freebsd || openbsd || netbsd || dragonfly || linux || windows)

package main

import "context"

func systemSource(context.Context) ([]Conn, error) { return nil, errUnsupported }

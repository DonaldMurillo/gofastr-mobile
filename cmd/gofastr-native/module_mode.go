package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

// readOnlyModuleFlag keeps diagnostic queries from updating go.mod or go.sum,
// while respecting a consuming app's vendor directory outside workspace mode.
func readOnlyModuleFlag(ctx context.Context, dir string) string {
	query := exec.CommandContext(ctx, "go", "env", "-json", "GOMOD", "GOWORK")
	query.Dir = dir
	out, err := query.Output()
	if err != nil {
		return "-mod=readonly"
	}
	var env struct{ GOMOD, GOWORK string }
	if json.Unmarshal(out, &env) != nil || env.GOWORK != "" && env.GOWORK != "off" || env.GOMOD == "" || env.GOMOD == os.DevNull {
		return "-mod=readonly"
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(env.GOMOD), "vendor", "modules.txt")); err == nil {
		return "-mod=vendor"
	}
	return "-mod=readonly"
}

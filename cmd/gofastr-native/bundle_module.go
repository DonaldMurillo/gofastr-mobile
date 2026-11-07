package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// buildBundleModule repacks aapt2's --proto-format output, the dex file
// and the native libraries into the base module zip bundletool expects:
//
//	AndroidManifest.xml -> manifest/AndroidManifest.xml
//	resources.pb        -> resources.pb
//	res/**              -> res/**
//	classes.dex         -> dex/classes.dex
//	jni/<abi>/*.so      -> lib/<abi>/*.so
//
// Any other entry in the proto APK is an error: nothing is dropped
// silently, and no entry name can escape the module.
func buildBundleModule(protoAPK, dexFile, jniDir string, abis []string, dst string) error {
	zr, err := zip.OpenReader(protoAPK)
	if err != nil {
		return err
	}
	defer zr.Close()
	type entry struct {
		name string
		file *zip.File
		src  string
	}
	var entries []entry
	seen := map[string]bool{}
	add := func(e entry) error {
		if seen[e.name] {
			return fmt.Errorf("bundle module entry %s appears twice", e.name)
		}
		seen[e.name] = true
		entries = append(entries, e)
		return nil
	}
	for _, f := range zr.File {
		name, err := moduleEntryName(f.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue // directory entry
		}
		if err := add(entry{name: name, file: f}); err != nil {
			return err
		}
	}
	if !seen["manifest/AndroidManifest.xml"] || !seen["resources.pb"] {
		return fmt.Errorf("%s is not aapt2 --proto-format output (no AndroidManifest.xml or resources.pb)", protoAPK)
	}
	if err := add(entry{name: "dex/classes.dex", src: dexFile}); err != nil {
		return err
	}
	libs, err := listFiles(jniDir, ".so")
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for _, lib := range libs {
		rel, err := filepath.Rel(jniDir, lib)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		abi, file, ok := strings.Cut(rel, "/")
		if !ok || strings.Contains(file, "/") || !contains(abis, abi) {
			return fmt.Errorf("unexpected native library %s (selected ABIs: %v)", rel, abis)
		}
		found[abi] = true
		if err := add(entry{name: "lib/" + rel, src: lib}); err != nil {
			return err
		}
	}
	for _, abi := range abis {
		if !found[abi] {
			return fmt.Errorf("no native library for %s under %s", abi, jniDir)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	fail := func(err error) error {
		zw.Close()
		out.Close()
		os.Remove(dst)
		return err
	}
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate})
		if err != nil {
			return fail(err)
		}
		var rc io.ReadCloser
		if e.file != nil {
			rc, err = e.file.Open()
		} else {
			rc, err = os.Open(e.src)
		}
		if err != nil {
			return fail(err)
		}
		_, err = io.Copy(w, io.LimitReader(rc, 1<<30))
		rc.Close()
		if err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// moduleEntryName maps one proto APK entry to its module path. It returns
// "" for a directory entry and an error for anything unexpected.
func moduleEntryName(name string) (string, error) {
	if strings.HasSuffix(name, "/") {
		return "", nil
	}
	clean := path.Clean(name)
	if clean != name || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(name, `\`) {
		return "", fmt.Errorf("proto APK entry %q is not a clean relative path", name)
	}
	switch {
	case clean == "AndroidManifest.xml":
		return "manifest/AndroidManifest.xml", nil
	case clean == "resources.pb":
		return clean, nil
	case strings.HasPrefix(clean, "res/"):
		return clean, nil
	}
	return "", fmt.Errorf("unexpected proto APK entry %q: the bundle module has no place for it", name)
}

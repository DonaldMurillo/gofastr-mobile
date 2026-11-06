package main

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validateDeviceFeatures(a app, platform string) error {
	if (a.ReceiveShares || a.SecureStorage || a.Widgets || a.AppGroup != "" || a.BiometricUsage != "") && a.Renderer != "native" {
		return errors.New("device feature flags require --renderer native")
	}
	if a.Widgets && a.URLScheme == "" {
		return errors.New("--widgets requires --url-scheme for widget navigation")
	}
	if a.AppGroup != "" && (!strings.HasPrefix(a.AppGroup, "group.") || len(a.AppGroup) > 200 || !appIDRe.MatchString(a.AppGroup)) {
		return errors.New("--app-group must be a reverse-DNS identifier starting with group.")
	}
	if platform == "ios-sim" && (a.ReceiveShares || a.Widgets) && a.AppGroup == "" {
		return errors.New("iOS share and widget extensions require --app-group")
	}
	if len(a.BiometricUsage) > 512 || !utf8.ValidString(a.BiometricUsage) || strings.ContainsFunc(a.BiometricUsage, unicode.IsControl) {
		return errors.New("--biometric-usage must be at most 512 UTF-8 bytes without control characters")
	}
	if a.BiometricUsage != "" && !a.SecureStorage {
		return errors.New("--biometric-usage requires --secure-storage")
	}
	if platform == "ios-sim" && a.SecureStorage && a.BiometricUsage == "" {
		return errors.New("iOS secure storage requires --biometric-usage for Face ID authentication")
	}
	return nil
}

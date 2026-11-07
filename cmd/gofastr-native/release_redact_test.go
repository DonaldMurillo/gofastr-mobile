package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// redactAll writes chunks through a fresh redactWriter, flushes, and
// returns what reached the underlying writer.
func redactAll(t *testing.T, secrets []string, chunks ...string) string {
	t.Helper()
	var b bytes.Buffer
	w := newRedactWriter(&b, secrets)
	for _, c := range chunks {
		if n, err := w.Write([]byte(c)); err != nil || n != len(c) {
			t.Fatalf("Write(%q) = %d, %v", c, n, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func byteChunks(s string) []string {
	out := make([]string, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = s[i : i+1]
	}
	return out
}

func TestRedactWriterEverySplit(t *testing.T) {
	secrets := []string{"hunter22", "pässwörd\nline2"} // multiline UTF-8
	input := "keytool: bad password hunter22 for key; retry pässwörd\nline2 done\n"
	want := "keytool: bad password [redacted] for key; retry [redacted] done\n"
	if got := redactAll(t, secrets, input); got != want {
		t.Fatalf("one write: %q", got)
	}
	for i := 0; i <= len(input); i++ {
		if got := redactAll(t, secrets, input[:i], input[i:]); got != want {
			t.Fatalf("split at %d: %q", i, got)
		}
		for j := i; j <= len(input); j++ {
			if got := redactAll(t, secrets, input[:i], input[i:j], input[j:]); got != want {
				t.Fatalf("split at %d,%d: %q", i, j, got)
			}
		}
	}
	if got := redactAll(t, secrets, byteChunks(input)...); got != want {
		t.Fatalf("byte at a time: %q", got)
	}
}

func TestRedactWriterOverlapsAndRepeats(t *testing.T) {
	cases := []struct {
		secrets []string
		in      string
		want    string
	}{
		// A shorter secret that prefixes a longer one must not leave the
		// longer one's tail visible.
		{[]string{"abcd", "abcdefgh"}, "x abcdefgh y abcd z", "x [redacted] y [redacted] z"},
		// Overlapping different secrets cover their union.
		{[]string{"ab", "bcdef"}, "-abcdef-", "-[redacted]-"},
		{[]string{"xyz1", "z123"}, "<xyz123>", "<[redacted]>"},
		// Self-overlapping repeats.
		{[]string{"aab"}, "aaab aab", "a[redacted] [redacted]"},
		{[]string{"abab"}, "ababab", "[redacted]"},
		// Duplicates and empty values are ignored, not fatal.
		{[]string{"", "s3cr3t", "s3cr3t"}, "s3cr3ts3cr3t!", "[redacted]!"},
		// A near miss at the end is ordinary output and must be kept.
		{[]string{"hunter22"}, "almost hunter2", "almost hunter2"},
		// A match that ends the stream.
		{[]string{"hunter22"}, "end hunter22", "end [redacted]"},
	}
	for _, c := range cases {
		if got := redactAll(t, c.secrets, c.in); got != c.want {
			t.Errorf("%v %q: one write %q, want %q", c.secrets, c.in, got, c.want)
		}
		if got := redactAll(t, c.secrets, byteChunks(c.in)...); got != c.want {
			t.Errorf("%v %q: byte at a time %q, want %q", c.secrets, c.in, got, c.want)
		}
		for i := 0; i <= len(c.in); i++ {
			if got := redactAll(t, c.secrets, c.in[:i], c.in[i:]); got != c.want {
				t.Errorf("%v %q: split %d %q, want %q", c.secrets, c.in, i, got, c.want)
			}
		}
	}
}

func TestRedactWriterShortSecrets(t *testing.T) {
	if got := redactAll(t, []string{"q"}, byteChunks("a q b")...); got != "a [redacted] b" {
		t.Fatal(got)
	}
	if got := redactAll(t, []string{"ab"}, "ab", "c", "a", "b"); got != "[redacted]c[redacted]" {
		t.Fatal(got)
	}
	t.Setenv("GOFASTR_TEST_SHORT", "abc")
	t.Setenv("GOFASTR_TEST_ONE", "z")
	t.Setenv("GOFASTR_TEST_EMPTY", "")
	got := secretValues("GOFASTR_TEST_SHORT", "GOFASTR_TEST_ONE", "GOFASTR_TEST_EMPTY", "GOFASTR_TEST_SHORT")
	if strings.Join(got, ",") != "abc,z" {
		t.Fatalf("secretValues = %q: every nonempty value must be redacted", got)
	}
}

func TestRedactWriterHoldsOnlyAPossibleSecret(t *testing.T) {
	var b bytes.Buffer
	w := newRedactWriter(&b, []string{"hunter22"})
	w.Write([]byte("progress 50%\n"))
	if b.String() != "progress 50%\n" {
		t.Fatalf("plain output was held back: %q", b.String())
	}
	w.Write([]byte("pass: hunt"))
	if b.String() != "progress 50%\npass: " {
		t.Fatalf("held %q", b.String())
	}
	w.Write([]byte(strings.Repeat("x", 1<<16)))
	if len(w.buf) >= 2*len("hunter22") {
		t.Fatalf("buffer kept %d bytes", len(w.buf))
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(b.String(), "pass: hunt"+strings.Repeat("x", 1<<16)) {
		t.Fatal("nonsensitive bytes were lost")
	}
}

func TestRedactWriterConcurrentWriters(t *testing.T) {
	var b bytes.Buffer
	w := newRedactWriter(&b, []string{"hunter22"})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				w.Write([]byte("line hunter22\n"))
			}
		}()
	}
	wg.Wait()
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "hunter") || strings.Count(b.String(), "[redacted]") != 1600 {
		t.Fatal("concurrent writes leaked or lost output")
	}
}

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

func TestRedactWriterPropagatesWriteAndFlushErrors(t *testing.T) {
	boom := errors.New("disk full")
	w := newRedactWriter(failWriter{boom}, []string{"hunter22"})
	if _, err := w.Write([]byte("plain\n")); !errors.Is(err, boom) {
		t.Fatalf("write error %v", err)
	}
	w = newRedactWriter(failWriter{boom}, []string{"hunter22"})
	if _, err := w.Write([]byte("hunt")); err != nil {
		t.Fatalf("a held-back prefix should not write yet: %v", err)
	}
	if err := w.Flush(); !errors.Is(err, boom) {
		t.Fatalf("flush error %v", err)
	}
}

func TestRedactError(t *testing.T) {
	secrets := []string{"hunter22"}
	if err := redactError(os.ErrNotExist, secrets); err != os.ErrNotExist {
		t.Fatal("an unrelated error was rewritten")
	}
	if err := redactError(nil, secrets); err != nil {
		t.Fatal(err)
	}
	inner := fmt.Errorf("keytool said hunter22: %w", context.Canceled)
	err := redactError(inner, secrets)
	if strings.Contains(err.Error(), "hunter22") || !strings.Contains(err.Error(), "keytool said [redacted]") {
		t.Fatal(err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("errors.Is no longer sees context.Canceled")
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("the redacted error exposes the original through Unwrap")
	}
	var target *redactedError
	if !errors.As(err, &target) || target.Error() != err.Error() {
		t.Fatal("errors.As")
	}
}

func TestRedactOutputRestoresAndRedacts(t *testing.T) {
	var b bytes.Buffer
	r := &runner{ctx: context.Background(), out: &b}
	helper := func() (err error) {
		defer redactOutput(r, []string{"hunter22"})(&err)
		if r.out == io.Writer(&b) {
			t.Fatal("r.out was not wrapped")
		}
		fmt.Fprint(r.out, "tool: password hun")
		fmt.Fprint(r.out, "ter22 rejected, tail hunt")
		return fmt.Errorf("signing with hunter22 failed: %w", os.ErrPermission)
	}
	err := helper()
	if r.out != io.Writer(&b) {
		t.Fatal("r.out was not restored after a failure")
	}
	if b.String() != "tool: password [redacted] rejected, tail hunt" {
		t.Fatalf("output %q", b.String())
	}
	if strings.Contains(err.Error(), "hunter22") || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error %v", err)
	}

	// Success restores too, and an unrelated error keeps its identity.
	b.Reset()
	ok := func() (err error) {
		defer redactOutput(r, []string{"hunter22"})(&err)
		fmt.Fprint(r.out, "done")
		return os.ErrNotExist
	}
	if err := ok(); err != os.ErrNotExist {
		t.Fatalf("identity lost: %v", err)
	}
	if r.out != io.Writer(&b) || b.String() != "done" {
		t.Fatalf("restore or flush: %q", b.String())
	}

	// A flush failure is reported, not dropped.
	boom := errors.New("pipe closed")
	r.out = failWriter{boom}
	flushFail := func() (err error) {
		defer redactOutput(r, []string{"hunter22"})(&err)
		fmt.Fprint(r.out, "hunt")
		return nil
	}
	if err := flushFail(); !errors.Is(err, boom) {
		t.Fatalf("flush error dropped: %v", err)
	}
}

func TestBuildAndroidReleaseRestoresOutputOnFailure(t *testing.T) {
	t.Setenv("ANDROID_HOME", t.TempDir()) // an empty SDK: the build fails at once
	t.Setenv("ANDROID_NDK_HOME", "")
	t.Setenv("GOFASTR_TEST_FAIL_PASS", "pw")
	var b bytes.Buffer
	r := &runner{ctx: context.Background(), out: &b}
	o := releaseOptions{profile: profileRelease, buildNumber: 2, apk: true,
		android: androidSigning{storePassEnv: "GOFASTR_TEST_FAIL_PASS", keyPassEnv: "GOFASTR_TEST_FAIL_PASS"}}
	if _, err := buildAndroidProfile(r, app{}, t.TempDir(), o); err == nil {
		t.Fatal("an empty SDK built")
	}
	if r.out != io.Writer(&b) {
		t.Fatal("r.out is still the redacting wrapper after a failed release build")
	}
	if _, err := buildAndroidProfile(r, app{}, t.TempDir(), debugOptions()); err == nil {
		t.Fatal("an empty SDK built")
	}
	if r.out != io.Writer(&b) {
		t.Fatal("the debug build changed r.out")
	}
}

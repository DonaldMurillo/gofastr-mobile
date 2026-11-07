//go:build !gofastr_debugtrace

package gallerynative

import "testing"

func TestObserverIsNilWithoutTag(t *testing.T) {
	if observer() != nil {
		t.Fatal("an ordinary build must not install a trace observer")
	}
}

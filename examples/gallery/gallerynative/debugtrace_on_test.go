//go:build gofastr_debugtrace

package gallerynative

import "testing"

func TestObserverIsSetWithTag(t *testing.T) {
	if observer() == nil {
		t.Fatal("the gofastr_debugtrace build must install the trace observer")
	}
}

package mobileui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

func TestPresentationOwnsItsComponents(t *testing.T) {
	for _, platform := range []Platform{IOS, Android} {
		t.Run(string(platform), func(t *testing.T) {
			page := Screen(Page{Platform: platform, Title: `<script>alert("title")</script>`, Primary: Action{Label: "New note", Href: "javascript:alert(1)"}},
				List("Notes", "1", []Row{{Title: "<img src=x onerror=alert(1)>", Href: "javascript:alert(1)"}}))
			markup := string(page)
			for _, unsafe := range []string{"<script>", "<img ", `href="javascript:`} {
				if strings.Contains(markup, unsafe) {
					t.Fatalf("unsafe markup: %s", markup)
				}
			}
			if !strings.Contains(markup, `data-gfm-platform="`+string(platform)+`"`) {
				t.Fatal("platform was not rendered before hydration")
			}
			if names := registry.Scan(markup); len(names) != 1 || names[0] != "gfm-mobile" {
				t.Fatalf("component stylesheet markers = %v", names)
			}
		})
	}
	if Platform("canvas").Validate() == nil {
		t.Fatal("a web renderer silently accepted an unsupported platform")
	}
}

func TestEditorEscapesDocumentContent(t *testing.T) {
	markup := string(DocumentEditor(context.Background(), Editor{
		Title: `" autofocus onfocus="alert(1)`, Body: "</textarea><script>alert(1)</script>", Action: "/notes/save",
	}))
	if strings.Contains(markup, "<script>") || !strings.Contains(markup, `value="&quot; autofocus onfocus=&quot;alert(1)"`) {
		t.Fatalf("unescaped editor data: %s", markup)
	}
	for _, required := range []string{`for="note-title"`, `for="note-body"`, `aria-live="polite"`, `method="post"`} {
		if !strings.Contains(markup, required) {
			t.Errorf("missing editor contract %s", required)
		}
	}
}

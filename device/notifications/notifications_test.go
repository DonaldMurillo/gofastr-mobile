package notifications

import (
	"strings"
	"testing"
)

func TestRequestShapeAndDeviceLocalCalendar(t *testing.T) {
	valid := Request{Operation: Schedule, Identifier: "notes:owned.note-1", Title: "Reminder", Body: "Body\nline", Route: "/notes/1", Date: "2028-02-29", Time: "23:59"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{Cancel, Status} {
		if err := (Request{Operation: operation, Identifier: valid.Identifier}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutate := range []func(*Request){
		func(value *Request) { value.Operation = "permission" },
		func(value *Request) { value.Identifier = "../other" },
		func(value *Request) { value.Identifier = strings.Repeat("x", 129) },
		func(value *Request) { value.Date = "2027-02-29" },
		func(value *Request) { value.Date = "2028-2-29" },
		func(value *Request) { value.Time = "24:00" },
		func(value *Request) { value.Time = "9:00" },
		func(value *Request) { value.Route = "https://example.com" },
		func(value *Request) { value.Route = "//example.com" },
		func(value *Request) { value.Title = "bad\x00title" },
	} {
		invalid := valid
		mutate(&invalid)
		if invalid.Validate() == nil {
			t.Fatalf("invalid reminder accepted: %+v", invalid)
		}
	}
	valid.Date = "2000-01-01"
	if err := valid.Validate(); err != nil {
		t.Fatal("Go must not apply its own timezone/past-time policy", err)
	}
}

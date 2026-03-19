package ratelimit

import (
	"errors"
	"testing"
)

// FuzzClassifyError fuzzes the error-string classifier with arbitrary error
// messages, ensuring no panic and that every input maps to a valid ErrorClass.
func FuzzClassifyError(f *testing.F) {
	f.Add("connection refused")
	f.Add("context canceled")
	f.Add("deadline exceeded")
	f.Add("")
	f.Add("unexpected EOF")
	f.Add("\x00\xff")

	f.Fuzz(func(t *testing.T, msg string) {
		ec := ClassifyError(errors.New(msg))
		if ec < ErrorClassNone || ec > ErrorClassFatal {
			t.Fatalf("ClassifyError returned out-of-range ErrorClass %d for msg %q", ec, msg)
		}
	})
}

// FuzzClassifyStatusCode fuzzes the status code classifier across the full
// integer range, ensuring no panic and valid ErrorClass output for every input.
func FuzzClassifyStatusCode(f *testing.F) {
	f.Add(0)
	f.Add(200)
	f.Add(301)
	f.Add(400)
	f.Add(403)
	f.Add(404)
	f.Add(429)
	f.Add(500)
	f.Add(502)
	f.Add(503)
	f.Add(504)
	f.Add(-1)
	f.Add(99999)

	f.Fuzz(func(t *testing.T, code int) {
		ec := ClassifyStatusCode(code)
		if ec < ErrorClassNone || ec > ErrorClassFatal {
			t.Fatalf("ClassifyStatusCode returned out-of-range ErrorClass %d for code %d", ec, code)
		}
	})
}

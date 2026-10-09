package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzReplayRecord(f *testing.F) {
	f.Add([]byte(validReplayLine))
	for _, s := range []string{`null`, `{"replay":null}`, `{"x":1,"x":2}`, "\xff", `{"sequence":18446744073709551616}`, `{"body":"{\"nested\":{}}"}`} {
		f.Add([]byte(s))
	}
	for typ, url := range map[string]string{"http": "https://example.com", "browser": "https://example.com", "dns": "example.com", "websocket": "ws://example.com", "grpc": "grpc://example.com/pkg.Service/Method", "sftp": "sftp://example.com/file"} {
		var b bytes.Buffer
		if err := EncodeJSONL(json.NewEncoder(&b), capturedResult(typ, url)); err != nil {
			f.Fatal(err)
		}
		f.Add(bytes.TrimSpace(b.Bytes()))
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		if len(line) > MaxReplayLineBytes {
			return
		}
		r, err := DecodeReplayRecord(line)
		if err != nil {
			return
		}
		if r.Envelope.Version != 1 || r.Envelope.Sequence == 0 || !replayRunID.MatchString(r.Envelope.RunID) {
			t.Fatal("accepted invalid identity")
		}
		if r.Envelope.Replayable {
			if r.Envelope.Request == nil {
				t.Fatal("missing request")
			}
			if err := validateReplayRequest(*r.Envelope.Request); err != nil {
				t.Fatal(err)
			}
		} else if r.Envelope.Request != nil || r.Envelope.Reason == "" {
			t.Fatal("invalid non-replayable envelope")
		}
	})
}

package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func FuzzReplayRecord(f *testing.F) {
	f.Add([]byte(validReplayLine))
	for _, policy := range []string{"0", "1", "2", "3", "null", `"2"`, "2.0"} {
		v2 := strings.Replace(validReplayLine, `"version":1`, `"version":2`, 1)
		f.Add([]byte(strings.Replace(v2, `"method":"POST"`, `"http_version":`+policy+`,"method":"POST"`, 1)))
	}
	f.Add([]byte(strings.Replace(validReplayLine, `"body":"{}"`, `"body":"\ud800"`, 1)))
	f.Add([]byte(strings.Replace(validReplayLine, "2026-10-08T21:16:33.123456789Z", "2026-10-08T21:16:33+24:00", 1)))
	for _, s := range []string{`null`, `{"replay":null}`, `{"x":1,"x":2}`, "\xff", `{"sequence":18446744073709551616}`, `{"body":"{\"nested\":{}}"}`} {
		f.Add([]byte(s))
	}
	for typ, url := range map[string]string{"http": "https://example.com", "browser": "https://example.com", "dns": "example.com", "websocket": "ws://example.com", "grpc": "grpc://example.com/pkg.Service/Method", "sftp": "sftp://example.com/file"} {
		var b bytes.Buffer
		if err := EncodeJSONL(json.NewEncoder(&b), capturedResult(typ, url)); err != nil {
			f.Fatal(err)
		}
		f.Add(bytes.TrimSpace(b.Bytes()))
		if typ == "websocket" {
			f.Add([]byte(strings.Replace(b.String(), `"send_messages":[]`, `"send_messages":[null]`, 1)))
		}
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		if len(line) > MaxReplayLineBytes {
			return
		}
		r, err := DecodeReplayRecord(line)
		if err != nil {
			return
		}
		if (r.Envelope.Version != 1 && r.Envelope.Version != 2) || r.Envelope.Sequence == 0 || !replayRunID.MatchString(r.Envelope.RunID) {
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

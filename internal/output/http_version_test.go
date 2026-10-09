package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestHTTPVersionReplayRoundTrip(t *testing.T) {
	for _, v := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			r := capturedResult("http", "https://example.com")
			r.Task.Config.HTTP.HTTPVersion = v
			r.Meta = map[string]string{"http_protocol": "HTTP/2.0"}
			var b bytes.Buffer
			if err := EncodeJSONL(json.NewEncoder(&b), r); err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(b.Bytes(), []byte(`"http_protocol":"HTTP/2.0"`)) {
				t.Fatal("protocol metadata missing from JSONL")
			}
			records, err := ReadReplay(&b)
			if err != nil {
				t.Fatal(err)
			}
			if records[0].Envelope.Version != 2 || records[0].Envelope.Request.Task().Config.HTTP.HTTPVersion != v {
				t.Fatalf("lost requested policy: %+v", records[0])
			}
		})
	}
	old, err := DecodeReplayRecord([]byte(validReplayLine))
	if err != nil {
		t.Fatal(err)
	}
	if old.Envelope.Version != 1 || old.Envelope.Request.Task().Config.HTTP.HTTPVersion != 0 {
		t.Fatal("v1 no longer automatic")
	}
	for typ, url := range map[string]string{"browser": "https://example.com", "dns": "example.com", "websocket": "ws://example.com", "grpc": "grpc://example.com/pkg.Service/Method", "sftp": "sftp://example.com/file"} {
		var b bytes.Buffer
		if err := EncodeJSONL(json.NewEncoder(&b), capturedResult(typ, url)); err != nil {
			t.Fatal(err)
		}
		records, err := ReadReplay(&b)
		if err != nil || records[0].Envelope.Version != 2 {
			t.Fatalf("%s new envelope: %v", typ, err)
		}
	}
}

func TestHTTPVersionReplayStrictSchemas(t *testing.T) {
	v2 := strings.Replace(validReplayLine, `"version":1`, `"version":2`, 1)
	for _, raw := range []string{"0", "1", "2", "-1", "3", "2.0", `"2"`, "true", "null", "18446744073709551616"} {
		line := strings.Replace(v2, `"method":"POST"`, `"http_version":`+raw+`,"method":"POST"`, 1)
		_, err := DecodeReplayRecord([]byte(line))
		valid := raw == "0" || raw == "1" || raw == "2"
		if (err == nil) != valid {
			t.Fatalf("v2 policy %s error %v", raw, err)
		}
	}
	for _, line := range []string{v2, strings.Replace(validReplayLine, `"method":"POST"`, `"http_version":0,"method":"POST"`, 1), strings.Replace(v2, `"version":2`, `"version":3`, 1)} {
		if _, err := DecodeReplayRecord([]byte(line)); err == nil {
			t.Fatal("invalid version schema accepted")
		}
	}
	plain := strings.ReplaceAll(strings.Replace(v2, `"method":"POST"`, `"http_version":2,"method":"POST"`, 1), "https://example.com", "http://example.com")
	if _, err := DecodeReplayRecord([]byte(plain)); err == nil {
		t.Fatal("plaintext forced H2 accepted")
	}
}

package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const validReplayLine = `{"url":"https://example.com","type":"http","status":503,"replay":{"version":1,"run_id":"405d88de-3fb2-42d8-bfb7-f365907ed78c","sequence":9007199254740993,"started_at":"2026-10-08T21:16:33.123456789Z","replayable":true,"request":{"url":"https://example.com","type":"http","http":{"method":"POST","body":"{}","timeout_s":15,"allow_cross_host_redirects":false}}}}`

func TestDecodeReplayRecord(t *testing.T) {
	r, err := DecodeReplayRecord([]byte(validReplayLine))
	if err != nil {
		t.Fatal(err)
	}
	if r.Envelope.Sequence != 9007199254740993 || r.Envelope.Request.HTTP.Body != "{}" {
		t.Fatalf("%+v", r)
	}
	for _, suffix := range []string{"", "\n", "\r\n"} {
		rs, err := ReadReplay(strings.NewReader(validReplayLine + suffix))
		if err != nil || len(rs) != 1 {
			t.Fatalf("suffix %q: %v", suffix, err)
		}
	}
	if rs, err := ReadReplay(strings.NewReader("")); err != nil || len(rs) != 0 {
		t.Fatalf("empty: %v", err)
	}
}

func TestDecodeReplayRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"legacy": `{"url":"https://example.com","type":"http","status":200}`,
		"null":   "null", "array": "[]", "blank": " ", "trailing": validReplayLine + "{}",
		"null envelope":     strings.Replace(validReplayLine, `"version":1`, `"version":null`, 1),
		"version":           strings.Replace(validReplayLine, `"version":1`, `"version":2`, 1),
		"uuid":              strings.Replace(validReplayLine, "405d88de", "405D88DE", 1),
		"fraction":          strings.Replace(validReplayLine, "9007199254740993", "1.5", 1),
		"overflow":          strings.Replace(validReplayLine, "9007199254740993", "18446744073709551616", 1),
		"negative":          strings.Replace(validReplayLine, "9007199254740993", "-1", 1),
		"zero":              strings.Replace(validReplayLine, "9007199254740993", "0", 1),
		"timestamp":         strings.Replace(validReplayLine, "2026-10-08T21:16:33.123456789Z", "secret-marker", 1),
		"duplicate":         strings.Replace(validReplayLine, `"version":1`, `"version":1,"version":1`, 1),
		"escaped duplicate": strings.Replace(validReplayLine, `"version":1`, `"version":1,"ver\u0073ion":1`, 1),
		"nested duplicate":  strings.Replace(validReplayLine, `"status":503`, `"extra":{"a":1,"a":2},"status":503`, 1),
		"unknown":           strings.Replace(validReplayLine, `"version":1`, `"secret-marker":1,"version":1`, 1),
		"missing bool":      strings.Replace(validReplayLine, `,"allow_cross_host_redirects":false`, "", 1),
		"null bool":         strings.Replace(validReplayLine, `"allow_cross_host_redirects":false`, `"allow_cross_host_redirects":null`, 1),
		"null request":      strings.Replace(validReplayLine, `"replayable":true`, `"replayable":false`, 1),
		"auth injection":    strings.Replace(validReplayLine, `"method":"POST"`, `"headers":{"X-Key":"secret-marker"},"method":"POST"`, 1),
		"reason conflict":   strings.Replace(validReplayLine, `"replayable":true`, `"reason":"auth_configured","replayable":true`, 1),
		"mismatch":          strings.Replace(validReplayLine, `"url":"https://example.com"`, `"url":"https://different.example"`, 1),
		"method":            strings.Replace(validReplayLine, `"method":"POST"`, `"method":"bad method"`, 1),
		"timeout":           strings.Replace(validReplayLine, `"timeout_s":15`, `"timeout_s":9223372036854775807`, 1),
		"negative timeout":  strings.Replace(validReplayLine, `"timeout_s":15`, `"timeout_s":-1`, 1),
		"userinfo":          strings.ReplaceAll(validReplayLine, "https://example.com", "https://secret-marker@example.com"),
		"bad port":          strings.ReplaceAll(validReplayLine, "https://example.com", "https://example.com:65536"),
		"unknown type":      strings.ReplaceAll(validReplayLine, `"type":"http"`, `"type":"alien"`),
		"invalid utf8":      strings.Replace(validReplayLine, `"body":"{}"`, "\"body\":\"\xff\"", 1),
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ReadReplay(strings.NewReader(line))
			if err == nil {
				t.Fatal("accepted invalid record")
			}
			if !strings.Contains(err.Error(), "line 1") || strings.Contains(err.Error(), "secret-marker") {
				t.Fatalf("unsafe or unlocated error: %v", err)
			}
		})
	}
}

func TestReadReplayRunOrderAndLimits(t *testing.T) {
	first := strings.Replace(validReplayLine, "9007199254740993", "1", 1)
	second := strings.Replace(first, `"sequence":1`, `"sequence":3`, 1)
	second = strings.Replace(second, "33.123456789", "34.123456789", 1)
	rs, err := ReadReplay(strings.NewReader(second + "\n" + first))
	if err != nil {
		t.Fatal(err)
	}
	if rs[0].Line != 2 || rs[1].Line != 1 || rs[1].Envelope.StartedAt.Sub(rs[0].Envelope.StartedAt) != time.Second {
		t.Fatal(rs)
	}
	for name, input := range map[string]string{
		"duplicate":     first + "\n" + first,
		"mixed":         first + "\n" + strings.Replace(second, "405d88de", "505d88de", 1),
		"backwards":     first + "\n" + strings.Replace(second, "34.123456789", "32.123456789", 1),
		"span overflow": strings.Replace(first, "2026-10-08", "0001-10-08", 1) + "\n" + second,
		"bad last line": first + "\n{bad}",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadReplay(strings.NewReader(input)); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	large := strings.Replace(validReplayLine, `"body":"{}"`, `"body":"`+strings.Repeat("x", 70<<10)+`"`, 1)
	if _, err := ReadReplay(strings.NewReader(large)); err != nil {
		t.Fatal(err)
	}
	// Exercise exact public line boundary without large decoded request bodies.
	pad := MaxReplayLineBytes - len(validReplayLine)
	exact := validReplayLine + strings.Repeat(" ", pad)
	if _, err := ReadReplay(strings.NewReader(exact + "\r\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadReplay(strings.NewReader(exact + " \n")); err == nil {
		t.Fatal("accepted oversized line")
	}
	var b strings.Builder
	for i := 1; i <= MaxReplayRecords; i++ {
		b.WriteString(strings.Replace(first, `"sequence":1`, fmt.Sprintf(`"sequence":%d`, i), 1))
		b.WriteByte('\n')
	}
	if rs, err := ReadReplay(strings.NewReader(b.String())); err != nil || len(rs) != MaxReplayRecords {
		t.Fatalf("count boundary: %v", err)
	}
	b.WriteString(first)
	if _, err := ReadReplay(strings.NewReader(b.String())); err == nil {
		t.Fatal("accepted too many records")
	}
	// Small private byte budget exercises the same LimitedReader path.
	if _, err := readReplay(strings.NewReader(first), int64(len(first))); err != nil {
		t.Fatal(err)
	}
	if _, err := readReplay(strings.NewReader(first+"\n"), int64(len(first))); err == nil {
		t.Fatal("accepted byte-budget overrun")
	}
}

func TestDecodeReplayCapturedTypes(t *testing.T) {
	for typ, url := range map[string]string{"http": "https://example.com", "browser": "https://example.com", "dns": "example.com", "websocket": "ws://example.com", "grpc": "grpc://example.com/pkg.Service/Method", "sftp": "sftp://example.com/file"} {
		t.Run(typ, func(t *testing.T) {
			r := capturedResult(typ, url)
			var b bytes.Buffer
			if err := EncodeJSONL(json.NewEncoder(&b), r); err != nil {
				t.Fatal(err)
			}
			rs, err := ReadReplay(&b)
			if err != nil {
				t.Fatal(err)
			}
			if rs[0].Envelope.Replayable != (typ != "sftp") {
				t.Fatal(rs)
			}
		})
	}
}

func TestReadReplayCheckedInExample(t *testing.T) {
	f, err := os.Open("../../config/replay-example.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := ReadReplay(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || !records[0].Envelope.Replayable || records[0].Envelope.Request.HTTP.Method != "POST" {
		t.Fatal("invalid documented fixture")
	}
}

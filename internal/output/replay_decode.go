package output

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"unicode/utf8"
)

const MaxReplayBytes = 256 << 20
const MaxReplayLineBytes = 8 << 20
const MaxReplayRecords = 10_000

var replayRunID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type ReplayRecord struct {
	Line     int
	URL      string
	Type     string
	Status   int
	Envelope ReplayEnvelope
}

// jsonUniqueKeys walks decoded tokens, so escaped spellings cannot hide duplicates.
// json.Valid bounds nesting depth before this walk is entered.
func jsonUniqueKeys(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate JSON key")
			}
			seen[name] = true
			if err := jsonUniqueKeys(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := jsonUniqueKeys(d); err != nil {
				return err
			}
		}
	}
	_, err = d.Token()
	return err
}

func replayObject(raw []byte, required, optional []string, strict bool, path string) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	for _, key := range required {
		v, ok := obj[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, fmt.Errorf("%s.%s is required and non-null", path, key)
		}
	}
	if strict {
		for key := range obj {
			if !slices.Contains(required, key) && !slices.Contains(optional, key) {
				return nil, fmt.Errorf("%s contains an unsupported field", path)
			}
		}
	}
	return obj, nil
}

// DecodeReplayRecord validates one executable record without performing I/O.
func DecodeReplayRecord(line []byte) (ReplayRecord, error) {
	var r ReplayRecord
	fail := func(field string) (ReplayRecord, error) { return r, fmt.Errorf("%s is invalid", field) }
	if len(line) > MaxReplayLineBytes {
		return fail("record size")
	}
	if !utf8.Valid(line) || !json.Valid(line) {
		return fail("JSON")
	}
	d := json.NewDecoder(bytes.NewReader(line))
	d.UseNumber()
	if err := jsonUniqueKeys(d); err != nil {
		return fail("JSON keys")
	}
	top, err := replayObject(line, []string{"url", "type", "status", "replay"}, nil, false, "record")
	if err != nil {
		return r, err
	}
	if json.Unmarshal(top["url"], &r.URL) != nil || json.Unmarshal(top["type"], &r.Type) != nil || json.Unmarshal(top["status"], &r.Status) != nil {
		return fail("record url/type/status")
	}
	if !slices.Contains([]string{"http", "browser", "dns", "websocket", "grpc", "sftp"}, r.Type) {
		return fail("record.type")
	}
	env, err := replayObject(top["replay"], []string{"version", "run_id", "sequence", "started_at", "replayable"}, []string{"reason", "request"}, true, "replay")
	if err != nil {
		return r, err
	}
	if json.Unmarshal(top["replay"], &r.Envelope) != nil {
		return fail("replay fields")
	}
	e := r.Envelope
	if e.Version != 1 {
		return fail("replay.version (supported: 1)")
	}
	if !replayRunID.MatchString(e.RunID) {
		return fail("replay.run_id")
	}
	if e.Sequence == 0 {
		return fail("replay.sequence")
	}
	if e.StartedAt.IsZero() {
		return fail("replay.started_at")
	}
	if !e.Replayable {
		if _, exists := env["request"]; exists {
			return fail("replay.request (forbidden for non-replayable records)")
		}
		if !slices.Contains([]string{"auth_configured", "custom_headers", "url_userinfo", "unsupported_type", "invalid_request"}, e.Reason) {
			return fail("replay.reason")
		}
		return r, nil
	}
	if _, exists := env["reason"]; exists {
		return fail("replay.reason (forbidden for replayable records)")
	}
	if r.Type == "sftp" {
		return fail("replay.request.type")
	}
	req, err := replayObject(env["request"], []string{"url", "type", r.Type}, nil, true, "replay.request")
	if err != nil {
		return r, err
	}
	fields := map[string][]string{
		"http":      {"method", "body", "timeout_s", "allow_cross_host_redirects"},
		"browser":   {"scroll", "wait_for_selector", "timeout_s"},
		"dns":       {"resolver", "record_type"},
		"websocket": {"duration_s", "send_messages", "expect_messages"},
		"grpc":      {"body", "timeout_s", "tls", "insecure"},
	}
	if _, err := replayObject(req[r.Type], fields[r.Type], nil, true, "replay.request."+r.Type); err != nil {
		return r, err
	}
	if e.Request == nil || e.Request.URL != r.URL || e.Request.Type != r.Type {
		return fail("replay.request url/type consistency")
	}
	if err := validateReplayRequest(*e.Request); err != nil {
		return r, err
	}
	return r, nil
}

// ReadReplay preflights the entire bounded input, returning dispatch order.
func ReadReplay(r io.Reader) ([]ReplayRecord, error) { return readReplay(r, MaxReplayBytes) }

func readReplay(r io.Reader, maxBytes int64) ([]ReplayRecord, error) {
	limited := &io.LimitedReader{R: r, N: maxBytes + 1}
	reader := bufio.NewReader(limited)
	var records []ReplayRecord
	lineNo := 1
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if limited.N == 0 {
			return nil, fmt.Errorf("line %d: input exceeds byte limit", lineNo)
		}
		if len(line)+len(fragment) > MaxReplayLineBytes+2 {
			return nil, fmt.Errorf("line %d: record exceeds size limit", lineNo)
		}
		line = append(line, fragment...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("line %d: reading replay input: %w", lineNo, err)
		}
		if err == io.EOF && len(line) == 0 {
			break
		}
		if len(records) == MaxReplayRecords {
			return nil, fmt.Errorf("line %d: too many replay records", lineNo)
		}
		if bytes.HasSuffix(line, []byte("\n")) {
			line = line[:len(line)-1]
			if bytes.HasSuffix(line, []byte("\r")) {
				line = line[:len(line)-1]
			}
		}
		record, decodeErr := DecodeReplayRecord(line)
		if decodeErr != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, decodeErr)
		}
		record.Line = lineNo
		if len(records) > 0 && record.Envelope.RunID != records[0].Envelope.RunID {
			return nil, fmt.Errorf("line %d: mixed capture runs are not supported", lineNo)
		}
		records = append(records, record)
		line = line[:0]
		lineNo++
		if err == io.EOF {
			break
		}
	}
	slices.SortFunc(records, func(a, b ReplayRecord) int { return cmp.Compare(a.Envelope.Sequence, b.Envelope.Sequence) })
	for i := 1; i < len(records); i++ {
		previous, current := records[i-1], records[i]
		if current.Envelope.Sequence == previous.Envelope.Sequence {
			return nil, fmt.Errorf("line %d: duplicate replay.sequence", current.Line)
		}
		if current.Envelope.StartedAt.Before(previous.Envelope.StartedAt) {
			return nil, fmt.Errorf("line %d: replay.started_at decreases in sequence order", current.Line)
		}
	}
	if len(records) > 1 {
		first, last := records[0].Envelope.StartedAt, records[len(records)-1].Envelope.StartedAt
		if !first.Add(last.Sub(first)).Equal(last) {
			return nil, fmt.Errorf("line %d: capture time span overflows duration", records[len(records)-1].Line)
		}
	}
	return records, nil
}

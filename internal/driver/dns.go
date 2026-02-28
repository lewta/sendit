package driver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lewta/sendit/internal/task"
	"github.com/miekg/dns"
)

// rcodeToHTTP maps a DNS RCODE to an HTTP-like status code so the engine's
// ClassifyStatusCode logic works correctly for DNS results.
//
//	NOERROR  (0) → 200
//	NXDOMAIN (3) → 404
//	REFUSED  (5) → 403
//	SERVFAIL (2) → 503
//	other        → 502
func rcodeToHTTP(rcode int) int {
	switch rcode {
	case dns.RcodeSuccess: // 0
		return 200
	case dns.RcodeNameError: // 3 NXDOMAIN
		return 404
	case dns.RcodeRefused: // 5
		return 403
	case dns.RcodeServerFailure: // 2
		return 503
	default:
		return 502
	}
}

// DNSDriver performs DNS lookups using the miekg/dns library.
type DNSDriver struct{}

// NewDNSDriver creates a DNSDriver.
func NewDNSDriver() *DNSDriver {
	return &DNSDriver{}
}

// Execute performs a DNS query for t.URL using the configured resolver and record type.
func (d *DNSDriver) Execute(ctx context.Context, t task.Task) task.Result {
	cfg := t.Config.DNS

	resolver := cfg.Resolver
	if resolver == "" {
		resolver = "8.8.8.8:53"
	}

	recordType := strings.ToUpper(cfg.RecordType)
	if recordType == "" {
		recordType = "A"
	}

	qtype, ok := dns.StringToType[recordType]
	if !ok {
		return task.Result{Task: t, Error: fmt.Errorf("unknown DNS record type: %s", recordType)}
	}

	fqdn := dns.Fqdn(t.URL)

	msg := new(dns.Msg)
	msg.SetQuestion(fqdn, qtype)
	msg.RecursionDesired = true

	client := &dns.Client{
		Net:     "udp",
		Timeout: 10 * time.Second,
	}

	start := time.Now()

	// Use a goroutine so we can respect ctx cancellation.
	type dnsResult struct {
		resp *dns.Msg
		rtt  time.Duration
		err  error
	}
	ch := make(chan dnsResult, 1)

	go func() {
		resp, rtt, err := client.Exchange(msg, resolver)
		ch <- dnsResult{resp, rtt, err}
	}()

	select {
	case <-ctx.Done():
		return task.Result{Task: t, Duration: time.Since(start), Error: ctx.Err()}
	case r := <-ch:
		if r.err != nil {
			return task.Result{Task: t, Duration: time.Since(start), Error: r.err}
		}
		return task.Result{
			Task:       t,
			StatusCode: rcodeToHTTP(r.resp.Rcode),
			Duration:   r.rtt,
		}
	}
}

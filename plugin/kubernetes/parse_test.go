package kubernetes

import (
	"testing"

	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		query    string
		expected string // output from r.String()
	}{
		// valid SRV request
		{"_http._tcp.webs.mynamespace.svc.inter.webs.tests.", "http.tcp..webs.mynamespace.svc"},
		// wildcard acceptance
		{"*.any.*.any.svc.inter.webs.tests.", "*.any..*.any.svc"},
		// A request of endpoint
		{"1-2-3-4.webs.mynamespace.svc.inter.webs.tests.", "*.*.1-2-3-4.webs.mynamespace.svc"},
		// bare zone
		{"inter.webs.tests.", "....."},
		// bare svc type
		{"svc.inter.webs.tests.", "....."},
		// bare pod type
		{"pod.inter.webs.tests.", "....."},
	}
	for i, tc := range tests {
		m := new(dns.Msg)
		m.SetQuestion(tc.query, dns.TypeA)
		state := request.Request{Zone: zone, Req: m}

		r, e := parseRequest(state.Name(), state.Zone)
		if e != nil {
			t.Errorf("Test %d, expected no error, got '%v'.", i, e)
		}
		rs := r.String()
		if rs != tc.expected {
			t.Errorf("Test %d, expected (stringified) recordRequest: %s, got %s", i, tc.expected, rs)
		}
	}
}

func TestParseInvalidRequest(t *testing.T) {
	invalid := []string{
		"webs.mynamespace.pood.inter.webs.test.",                 // Request must be for pod or svc subdomain.
		"too.long.for.what.I.am.trying.to.pod.inter.webs.tests.", // Too long.
	}

	for i, query := range invalid {
		m := new(dns.Msg)
		m.SetQuestion(query, dns.TypeA)
		state := request.Request{Zone: zone, Req: m}

		if _, e := parseRequest(state.Name(), state.Zone); e == nil {
			t.Errorf("Test %d: expected error from %s, got none", i, query)
		}
	}
}

func TestParseRequestWildWarning(t *testing.T) {
	tests := []struct {
		query string
		warn  bool
	}{
		// non-wildcards
		{"webs.mynamespace.svc.inter.webs.tests.", false},            // service
		{"endpoint.webs.mynamespace.svc.inter.webs.tests.", false},   // endpoint
		{"_http._tcp.webs.mynamespace.svc.inter.webs.tests.", false}, // srv
		// wildcards
		{"*.webs.mynamespace.svc.inter.webs.tests.", true},       // wild endpoint name
		{"*._tcp.webs.mynamespace.svc.inter.webs.tests.", true},  // wild port
		{"_http.*.webs.mynamespace.svc.inter.webs.tests.", true}, // wild protocol
		{"*.mynamespace.svc.inter.webs.tests.", true},            // wild service name
		{"webs.*.svc.inter.webs.tests.", true},                   // wild namespace
	}
	for i, tc := range tests {
		m := new(dns.Msg)
		m.SetQuestion(tc.query, dns.TypeA)
		state := request.Request{Zone: zone, Req: m}

		var warned bool
		warnWild = func(wild *bool, name string) {
			warned = *wild
		}
		_, e := parseRequest(state.Name(), state.Zone)
		if e != nil {
			t.Errorf("Test %d, expected no error, got '%v'.", i, e)
		}

		if warned != tc.warn {
			t.Errorf("Test %d, expected warning: %v, got %v", i, tc.warn, warned)
		}
	}
}

const zone = "inter.webs.tests."

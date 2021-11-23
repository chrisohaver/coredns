package tsig

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// TSIGServer verifies tsig status and adds tsig to responses
type TSIGServer struct {
	Zones   []string
	secrets map[string]string // [key-name]secret
	types   qTypes
	all     bool
	Next    plugin.Handler
}

type qTypes map[uint16]struct{}

// Name implements plugin.Handler
func (t TSIGServer) Name() string { return pluginName }

// ServeDNS implements plugin.Handler
func (t *TSIGServer) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	var err error
	state := request.Request{Req: r, W: w}
	if z := plugin.Zones(t.Zones).Matches(state.Name()); z == "" {
		return plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
	}

	var tsigRR = r.IsTsig()
	rcode := dns.RcodeSuccess
	if !t.tsigRequired(state.QType()) && tsigRR == nil {
		return plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
	}

	if tsigRR == nil {
		log.Debugf("rejecting '%s' request without TSIG\n", dns.TypeToString[state.QType()])
		rcode = dns.RcodeRefused
	}

	// wrap the response writer so the response will be TSIG signed.
	w = &restoreTsigWriter{w, r, tsigRR}

	if err = w.TsigStatus(); err != nil {
		log.Debugf("TSIG validation failed: %v %v", dns.TypeToString[state.QType()], err)
		rcode = dns.RcodeNotAuth
		switch err {
		case dns.ErrSecret:
			tsigRR.Error = dns.RcodeBadKey
			// See RFC-2845: 4.7. Special considerations for forwarding servers
			// We need to leave the TSIG alone/intact when forwarding.  This can be handled by proper corefile zone
			// config, i.e. tsig plugin zone should not contain a forwarded zone.
		case dns.ErrTime:
			tsigRR.Error = dns.RcodeBadTime
		default:
			tsigRR.Error = dns.RcodeBadSig
		}
		resp := new(dns.Msg).SetRcode(r, rcode)
		w.WriteMsg(resp)
		return dns.RcodeSuccess, nil
	}

	// strip the TSIG RR. Next, and subsequent plugins will not see the TSIG RRs.
	if len(r.Extra) > 1 {
		r.Extra = r.Extra[0 : len(r.Extra)-1]
	} else {
		r.Extra = []dns.RR{}
	}

	if rcode == dns.RcodeSuccess {
		rcode, err = plugin.NextOrFailure(t.Name(), t.Next, ctx, w, r)
		if err != nil {
			log.Errorf("request handler returned an error: %v\n", err)
		}
	}
	// If not written yet, we have to return the response here to make sure it's TSIG signed.
	if !plugin.ClientWrite(rcode) {
		resp := new(dns.Msg).SetRcode(r, rcode)
		w.WriteMsg(resp)
	}
	return dns.RcodeSuccess, nil
}

func (t *TSIGServer) tsigRequired(qtype uint16) bool {
	if t.all {
		return true
	}
	if _, ok := t.types[qtype]; ok {
		return true
	}
	return false
}

// restoreTsigWriter Implement Response Writer, and adds a TSIG RR to a response
type restoreTsigWriter struct {
	dns.ResponseWriter
	req     *dns.Msg  // original request excluding TSIG if it has one
	reqTSIG *dns.TSIG // original TSIG
}

// WriteMsg adds a TSIG RR to the response
func (r *restoreTsigWriter) WriteMsg(m *dns.Msg) error {
	// Make sure the response has an EDNS OPT RR if the request had it.
	// Otherwise ScrubWriter would append it *after* TSIG, making it a non-compliant DNS message.
	state := request.Request{Req: r.req, W: r.ResponseWriter}
	state.SizeAndDo(m)

	repTSIG := m.IsTsig()
	if r.reqTSIG != nil && repTSIG == nil {
		repTSIG = new(dns.TSIG)
		repTSIG.Hdr = dns.RR_Header{Name: r.reqTSIG.Hdr.Name, Rrtype: dns.TypeTSIG, Class: dns.ClassANY}
		repTSIG.Algorithm = r.reqTSIG.Algorithm
		repTSIG.OrigId = m.MsgHdr.Id
		repTSIG.Error = r.reqTSIG.Error
		repTSIG.MAC = r.reqTSIG.MAC
		repTSIG.MACSize = r.reqTSIG.MACSize
		if repTSIG.Error == dns.RcodeBadTime {
			// per RFC 2854 4.5.2. client time goes into TimeSigned, server time in OtherData, OtherLen = 6 ...
			repTSIG.TimeSigned = r.reqTSIG.TimeSigned
			b := make([]byte, 8)
			// TimeSigned is network byte order.
			binary.BigEndian.PutUint64(b, uint64(time.Now().Unix()))
			// truncate to 48 least significant bits (network order 6 rightmost bytes)
			repTSIG.OtherData = hex.EncodeToString(b[2:])
			repTSIG.OtherLen = 6
		}
		// empty MAC for TSIG errors (except BadTime)
		if repTSIG.Error > 0 && repTSIG.Error != dns.RcodeBadTime {
			repTSIG.MAC = ""
			repTSIG.MACSize = 0
		}
		m.Extra = append(m.Extra, repTSIG)
	}

	return r.ResponseWriter.WriteMsg(m)
}

const pluginName = "tsig"

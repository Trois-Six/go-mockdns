package mockdns

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"
)

type Zone struct {
	// Return the specified error on any lookup using this zone.
	// For Server, non-nil value results in SERVFAIL response.
	Err error

	// When used with Server, set the Authenticated Data (AD) flag
	// in the responses.
	AD bool

	A     []string
	AAAA  []string
	TXT   []string
	PTR   []string
	CNAME string
	MX    []net.MX
	NS    []net.NS
	SRV   []net.SRV
}

// Resolver is the struct that implements interface same as net.Resolver
// and so can be used as a drop-in replacement for it if tested code
// supports it.
type Resolver struct {
	Zones map[string]Zone

	// Don't follow CNAME in Zones for Lookup*.
	SkipCNAME bool
}

func notFound(host string) error {
	return &net.DNSError{
		Err:        "no such host",
		Name:       host,
		Server:     "127.0.0.1:53",
		IsNotFound: true,
	}
}

func (r *Resolver) LookupAddr(ctx context.Context, addr string) (names []string, err error) {
	arpa, err := dns.ReverseAddr(addr)
	if err != nil {
		return nil, err
	}

	rzone, ok := r.Zones[arpa]
	if !ok {
		return nil, notFound(arpa)
	}

	return rzone.PTR, nil
}

func (r *Resolver) LookupCNAME(ctx context.Context, host string) (cname string, err error) {
	rzone, ok := r.Zones[strings.ToLower(host)]
	if !ok {
		return "", notFound(host)
	}

	return rzone.CNAME, nil
}

func (r *Resolver) LookupHost(ctx context.Context, host string) (addrs []string, err error) {
	_, addrs4, err := r.lookupA(ctx, host)
	if err != nil {
		return nil, err
	}
	_, addrs6, err := r.lookupAAAA(ctx, host)
	if err != nil {
		return nil, err
	}

	addrs = append(addrs, addrs4...)
	addrs = append(addrs, addrs6...)

	if len(addrs) == 0 {
		return nil, notFound(host)
	}

	return addrs, err
}

func (r *Resolver) targetZone(name string) (cname string, zone Zone, err error) {
	rzone, ok := r.Zones[strings.ToLower(dns.Fqdn(name))]
	if !ok {
		return "", Zone{}, notFound(name)
	}

	if rzone.Err != nil {
		return "", rzone, rzone.Err
	}

	cname = rzone.CNAME

	if !r.SkipCNAME {
		for rzone.CNAME != "" {
			rzone, ok = r.Zones[strings.ToLower(rzone.CNAME)]
			if !ok {
				return cname, Zone{}, notFound(rzone.CNAME)
			}
			if rzone.Err != nil {
				return "", rzone, rzone.Err
			}
		}
	}

	return cname, rzone, nil
}

func (r *Resolver) lookupA(ctx context.Context, host string) (cname string, addrs []string, err error) {
	cname, rzone, err := r.targetZone(host)
	if err != nil {
		return cname, nil, err
	}

	return cname, rzone.A, nil
}

func (r *Resolver) lookupAAAA(ctx context.Context, host string) (cname string, addrs []string, err error) {
	cname, rzone, err := r.targetZone(host)
	if err != nil {
		return cname, nil, err
	}

	return cname, rzone.AAAA, nil
}

func (r *Resolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	addrs, err := r.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}

	parsed := make([]net.IPAddr, 0, len(addrs))
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			return nil, fmt.Errorf("malformed IP in records: %v", addr)
		}

		parsed = append(parsed, net.IPAddr{IP: ip})
	}

	return parsed, nil
}

func (r *Resolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	_, mx, err := r.lookupMX(ctx, name)
	return mx, err
}

func (r *Resolver) lookupMX(ctx context.Context, name string) (string, []*net.MX, error) {
	cname, rzone, err := r.targetZone(name)
	if err != nil {
		return "", nil, err
	}

	out := make([]*net.MX, 0, len(rzone.MX))
	for _, mx := range rzone.MX {
		mxCpy := mx
		out = append(out, &mxCpy)
	}

	return cname, out, nil
}

func (r *Resolver) LookupNS(ctx context.Context, name string) ([]*net.NS, error) {
	_, ns, err := r.lookupNS(ctx, name)
	return ns, err
}

func (r *Resolver) lookupNS(ctx context.Context, name string) (string, []*net.NS, error) {
	cname, rzone, err := r.targetZone(name)
	if err != nil {
		return "", nil, err
	}

	out := make([]*net.NS, 0, len(rzone.MX))
	for _, ns := range rzone.NS {
		nsCpy := ns
		out = append(out, &nsCpy)
	}

	return cname, out, nil
}

func (r *Resolver) LookupPort(ctx context.Context, network, service string) (port int, err error) {
	// TODO: Check whether it can cause problems with net.DefaultResolver hjacking.
	return net.LookupPort(network, service)
}

func (r *Resolver) LookupSRV(ctx context.Context, service, proto, name string) (cname string, addrs []*net.SRV, err error) {
	query := fmt.Sprintf("_%s._%s.%s", service, proto, name)
	return r.lookupSRV(ctx, query)
}

func (r *Resolver) lookupSRV(ctx context.Context, query string) (cname string, addrs []*net.SRV, err error) {
	cname, rzone, err := r.targetZone(query)
	if err != nil {
		return "", nil, err
	}

	out := make([]*net.SRV, 0, len(rzone.SRV))
	for _, srv := range rzone.SRV {
		srvCpy := srv
		out = append(out, &srvCpy)
	}

	return cname, out, nil
}

func (r *Resolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	_, txt, err := r.lookupTXT(ctx, name)
	return txt, err
}

func (r *Resolver) lookupTXT(ctx context.Context, name string) (string, []string, error) {
	cname, rzone, err := r.targetZone(name)
	if err != nil {
		return "", nil, err
	}

	return cname, rzone.TXT, nil
}
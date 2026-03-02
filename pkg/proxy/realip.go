package proxy

import (
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

const (
	xForwardedFor = "X-Forwarded-For"
)

func getRemoteIp(r *http.Request) netip.AddrPort {
	host, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		panic(fmt.Errorf("invalid host port %q: %w", r.RemoteAddr, err))
	}

	return host
}

func fillRealIp(rCtx *RequestContext) {
	isTrusted := slices.ContainsFunc(rCtx.routingConfig.TrustedProxies, func(ip netip.Prefix) bool {
		return ip.Contains(rCtx.RemoteIp.Addr())
	})
	forwardedHeaderContent := rCtx.R.Header.Get("X-Forwarded-For")
	if forwardedHeaderContent == "" {
		rCtx.RealIp = rCtx.RemoteIp.Addr()
		return
	}

	if !isTrusted {
		rCtx.Log.Warn(fmt.Sprintf("received %s header from untrusted proxy %s",
			xForwardedFor, rCtx.RemoteIp.String()))
		rCtx.RealIp = rCtx.RemoteIp.Addr()
		return
	}

	ips := strings.Split(forwardedHeaderContent, ",")
	clientIpRaw := strings.TrimSpace(ips[0])
	clientIp, err := netip.ParseAddr(clientIpRaw)
	if err != nil {
		rCtx.Log.Warn(fmt.Sprintf("received invalid ip address %s in %s header from trusted proxy %s",
			clientIpRaw, xForwardedFor, rCtx.RemoteIp.String()))
		rCtx.RealIp = rCtx.RemoteIp.Addr()
		return
	}

	rCtx.RealIp = clientIp
}

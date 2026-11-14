package query

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
)

func isBlockedAddr(a netip.Addr) bool {
	if a.Is4In6() {
		a = a.Unmap()
	}
	if a.IsLoopback() || a.IsUnspecified() ||
		a.IsPrivate() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return true
	}
	// CGNAT 100.64.0.0/10 не покрывается IsPrivate
	if a.Is4() {
		b := a.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
	}
	return false
}

func IsBlockedHost(ctx context.Context, host string) bool {
	return Options{}.isBlocked(ctx, host)
}

func (o Options) isBlocked(ctx context.Context, host string) bool {
	if o.AllowLocalhost {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return isBlockedAddr(addr)
	}
	addrs, err := o.resolveHost(ctx, host)
	if err != nil {
		// Fail-closed: не резолвится — не пускаем.
		return true
	}
	return slices.ContainsFunc(addrs, isBlockedAddr)
}

func IsDialError(err error) bool {
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}
	switch opErr.Op {
	case "dial", "read", "write":
		return true
	}
	return false
}

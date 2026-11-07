package facts

import (
	"net/netip"
	"slices"
	"strings"
)

// virtualIfacePrefixes 是容器与虚拟网桥网卡的名字前缀, 它们的地址只在本机可达, 不展示。
var virtualIfacePrefixes = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "kube", "lxc", "lxd", "podman", "vEthernet (WSL"}

// parseAddrs 把 `<网卡> <地址>` 行整理成 IPv4 与 IPv6 两组可对外使用的地址。
// 跳过回环、链路本地、组播与虚拟网卡; 地址可带 `/前缀` 与 `%zone`。公网地址排在内网地址前, 其余保持网卡顺序。
func parseAddrs(lines []string) (v4, v6 []string) {
	seen := map[netip.Addr]bool{}
	var all []netip.Addr
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		iface := strings.Join(fields[:len(fields)-1], " ")
		if isVirtualIface(iface) {
			continue
		}
		raw := strings.TrimPrefix(fields[len(fields)-1], "addr:")
		raw, _, _ = strings.Cut(raw, "/")
		ip, err := netip.ParseAddr(raw)
		if err != nil {
			continue
		}
		ip = ip.WithZone("").Unmap()
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() || seen[ip] {
			continue
		}
		seen[ip] = true
		all = append(all, ip)
	}
	slices.SortStableFunc(all, func(a, b netip.Addr) int {
		return boolRank(a.IsPrivate()) - boolRank(b.IsPrivate())
	})
	for _, ip := range all {
		if ip.Is4() {
			v4 = append(v4, ip.String())
		} else {
			v6 = append(v6, ip.String())
		}
	}
	return v4, v6
}

func isVirtualIface(name string) bool {
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

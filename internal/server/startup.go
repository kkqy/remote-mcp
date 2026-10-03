package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
)

type Interface struct {
	Up        bool
	Addresses []netip.Addr
}
type Enumerate func() ([]Interface, error)

func LocalInterfaces() ([]Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	result := make([]Interface, 0, len(interfaces))
	for _, iface := range interfaces {
		item := Interface{Up: iface.Flags&net.FlagUp != 0}
		if !item.Up {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil {
				item.Addresses = append(item.Addresses, prefix.Addr())
			}
		}
		result = append(result, item)
	}
	return result, nil
}

// CandidateAddresses 只列出实际监听地址族；全接口不会生成 0.0.0.0 或 :: URL。
func CandidateAddresses(bound *net.TCPAddr, enumerate Enumerate) ([]netip.Addr, error) {
	addr, ok := netip.AddrFromSlice(bound.IP)
	if !ok {
		return nil, fmt.Errorf("监听器未返回 IP 地址")
	}
	addr = addr.Unmap()
	if bound.Zone != "" {
		addr = addr.WithZone(bound.Zone)
	}
	if !addr.IsUnspecified() {
		// 服务端的链路本地区域编号无法直接供远端客户端复制使用。
		if addr.IsMulticast() || (addr.Is6() && addr.IsLinkLocalUnicast()) {
			return nil, nil
		}
		return []netip.Addr{addr}, nil
	}
	interfaces, err := enumerate()
	if err != nil {
		return nil, err
	}
	seen := map[netip.Addr]bool{}
	for _, iface := range interfaces {
		if !iface.Up {
			continue
		}
		for _, ip := range iface.Addresses {
			ip = ip.Unmap()
			if !ip.IsValid() || ip.Is4() != addr.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLoopback() || (ip.Is6() && ip.IsLinkLocalUnicast()) {
				continue
			}
			seen[ip] = true
		}
	}
	result := make([]netip.Addr, 0, len(seen))
	for ip := range seen {
		result = append(result, ip)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Compare(result[j]) < 0 })
	return result, nil
}

type ClientEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}
type ClientConfig struct {
	Servers map[string]ClientEntry `json:"mcpServers"`
}

func WriteStartup(out, diagnostics io.Writer, bound *net.TCPAddr, secure bool, token string, enumerate Enumerate) error {
	addresses, err := CandidateAddresses(bound, enumerate)
	if err != nil {
		fmt.Fprintln(diagnostics, "无法枚举本机地址；服务已监听，请手动配置连接地址。")
		return nil
	}
	if len(addresses) == 0 {
		fmt.Fprintln(diagnostics, "没有匹配的可展示地址；服务已监听，请检查网卡和绑定设置。")
		return nil
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	fmt.Fprintln(diagnostics, "以下每段 JSON 是同一服务的备选配置，请选择 Agent 可达的一个；配置包含当前 Token。")
	for _, ip := range addresses {
		endpoint := url.URL{Scheme: scheme, Host: net.JoinHostPort(ip.String(), strconv.Itoa(bound.Port)), Path: "/mcp"}
		if ip.IsLoopback() {
			fmt.Fprintln(diagnostics, "回环地址仅供本机连接。")
		}
		conf := ClientConfig{Servers: map[string]ClientEntry{"remote-mcp": {Type: "http", URL: endpoint.String(), Headers: map[string]string{"Authorization": "Bearer " + token}}}}
		if err := enc.Encode(conf); err != nil {
			return fmt.Errorf("写入启动配置失败")
		}
	}
	return nil
}

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/miekg/dns"
)

const googleDoHURL = "https://dns.google/dns-query"

// 从各大 CDN 提取真实 IP
func getRealIP(r *http.Request) string {
	headersToCheck := []string{
		"CF-Connecting-IP", // Cloudflare
		"Fastly-Client-IP", // Fastly
		"EO-Client-IP",     // Tencent EdgeOne
		"True-Client-IP",   // Cloudflare/Akamai
		"X-Real-IP",        // Generic Nginx/ESA
	}

	for _, header := range headersToCheck {
		if ip := r.Header.Get(header); ip != "" {
			return strings.TrimSpace(ip)
		}
	}

	// 兜底处理 X-Forwarded-For (取第一个 IP)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// 如果前面没有代理，直接取 RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// 给 DNS 消息添加 ECS (EDNS Client Subnet)
func addECS(msg *dns.Msg, clientIP string) error {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", clientIP)
	}

	// 创建 ECS Option
	ecs := &dns.EDNS0_SUBNET{
		Code:          dns.EDNS0SUBNET,
		SourceScope:   0,
	}

	// 判断是 IPv4 还是 IPv6 并设置对应的掩码
	// 为了保护隐私且满足 Google 调度，IPv4 通常用 /24，IPv6 通常用 /56
	if ip4 := ip.To4(); ip4 != nil {
		ecs.Family = 1
		ecs.SourceNetmask = 24
		ecs.Address = ip4
	} else {
		ecs.Family = 2
		ecs.SourceNetmask = 56
		ecs.Address = ip
	}

	// 查找是否已经存在 OPT 记录 (EDNS)
	var opt *dns.OPT
	for _, extra := range msg.Extra {
		if o, ok := extra.(*dns.OPT); ok {
			opt = o
			break
		}
	}

	// 如果没有 OPT 记录，则创建一个
	if opt == nil {
		opt = new(dns.OPT)
		opt.Hdr.Name = "."
		opt.Hdr.Rrtype = dns.TypeOPT
		opt.SetUDPSize(4096)
		msg.Extra = append(msg.Extra, opt)
	}

	// 将 ECS 添加到 OPT 记录中 (先清除原有的 ECS 避免冲突)
	filteredOptions := make([]dns.EDNS0, 0)
	for _, option := range opt.Option {
		if option.Option() != dns.EDNS0SUBNET {
			filteredOptions = append(filteredOptions, option)
		}
	}
	opt.Option = append(filteredOptions, ecs)

	return nil
}

// 处理 DoH 请求
func handleDoH(w http.ResponseWriter, r *http.Request) {
	// 仅支持 POST 和 GET 方式的 application/dns-message，这里为了简便以 POST 为主示例
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/dns-message" {
		http.Error(w, "Only POST application/dns-message is supported", http.StatusUnsupportedMediaType)
		return
	}

	clientIP := getRealIP(r)
	log.Printf("Received request from IP: %s", clientIP)

	// 读取客户端发来的 DNS 请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// 解析为 DNS 报文
	msg := new(dns.Msg)
	if err := msg.Unpack(body); err != nil {
		http.Error(w, "Failed to unpack DNS message", http.StatusBadRequest)
		return
	}

	// 注入 ECS
	if err := addECS(msg, clientIP); err != nil {
		log.Printf("Failed to add ECS: %v", err)
	}

	// 重新打包为二进制
	packedMsg, err := msg.Pack()
	if err != nil {
		http.Error(w, "Failed to pack DNS message", http.StatusInternalServerError)
		return
	}

	// 发送给 Google DoH
	req, err := http.NewRequest(http.MethodPost, googleDoHURL, bytes.NewReader(packedMsg))
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to contact upstream DoH", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 将 Google 的响应原样返回给客户端
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read upstream response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/dns-message")
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func main() {
	http.HandleFunc("/dns-query", handleDoH)
	log.Println("DoH server starting on :8080...")
	// 生产环境中推荐绑定到 127.0.0.1，然后用 Nginx/Caddy 反代并加上 TLS
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
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

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// 给 DNS 消息添加 ECS
func addECS(msg *dns.Msg, clientIP string) error {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return fmt.Errorf("invalid IP address: %s", clientIP)
	}

	ecs := &dns.EDNS0_SUBNET{
		Code:        dns.EDNS0SUBNET,
		SourceScope: 0,
	}

	if ip4 := ip.To4(); ip4 != nil {
		ecs.Family = 1
		ecs.SourceNetmask = 24
		ecs.Address = ip4
	} else {
		ecs.Family = 2
		ecs.SourceNetmask = 56
		ecs.Address = ip
	}

	var opt *dns.OPT
	for _, extra := range msg.Extra {
		if o, ok := extra.(*dns.OPT); ok {
			opt = o
			break
		}
	}

	if opt == nil {
		opt = new(dns.OPT)
		opt.Hdr.Name = "."
		opt.Hdr.Rrtype = dns.TypeOPT
		opt.SetUDPSize(4096)
		msg.Extra = append(msg.Extra, opt)
	}

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
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/dns-message" {
		http.Error(w, "Only POST application/dns-message is supported", http.StatusUnsupportedMediaType)
		return
	}

	clientIP := getRealIP(r)
	log.Printf("Received request from IP: %s", clientIP)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(body); err != nil {
		http.Error(w, "Failed to unpack DNS message", http.StatusBadRequest)
		return
	}

	if err := addECS(msg, clientIP); err != nil {
		log.Printf("Failed to add ECS: %v", err)
	}

	packedMsg, err := msg.Pack()
	if err != nil {
		http.Error(w, "Failed to pack DNS message", http.StatusInternalServerError)
		return
	}

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
	// 【新增】：从环境变量读取路径，如果没有设置，则默认使用 /dns-query
	dohPath := os.Getenv("DOH_PATH")
	if dohPath == "" {
		dohPath = "/dns-query"
	}

	// 【安全防护】：确保路径以 "/" 开头，防止手滑填错
	if !strings.HasPrefix(dohPath, "/") {
		dohPath = "/" + dohPath
	}

	http.HandleFunc(dohPath, handleDoH)
	
	// 在日志中打印出当前监听的私有路径
	log.Printf("DoH server starting on :8080...")
	log.Printf("Private DoH Path is set to: %s", dohPath)
	
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

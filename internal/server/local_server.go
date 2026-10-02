package server

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/nbd-wtf/go-nostr"
	"laptopcontrol/internal/config"
	"laptopcontrol/internal/pairing"
	"laptopcontrol/internal/protocol"
	"laptopcontrol/internal/qr"
)

const DefaultMaxConcurrentControl = 8

type Server struct {
	cfg        *config.Config
	webFS      fs.FS
	port       int
	pairingMgr *pairing.Manager
	handler    *protocol.Handler
	controlSem chan struct{}
	mu         sync.RWMutex
}

func NewServer(cfg *config.Config, webFS fs.FS, port int, pairingMgr *pairing.Manager, handler *protocol.Handler) *Server {
	return &Server{
		cfg:        cfg,
		webFS:      webFS,
		port:       port,
		pairingMgr: pairingMgr,
		handler:    handler,
		controlSem: make(chan struct{}, DefaultMaxConcurrentControl),
	}
}

// SetControlConcurrency overrides the concurrency limit on /api/control (useful for tests)
func (s *Server) SetControlConcurrency(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = DefaultMaxConcurrentControl
	}
	s.controlSem = make(chan struct{}, limit)
}

// GetLocalIPv4 returns the preferred outbound LAN IPv4 address
func GetLocalIPv4() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String()
	}

	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return ipnet.IP.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

const DefaultCloudflareURL = "https://pc-remote-45t.pages.dev"

// GetPairingURL generates the complete pairing URL pointing to your Cloudflare Pages PWA
func (s *Server) GetPairingURL() string {
	ip := GetLocalIPv4()
	token := ""
	if s.pairingMgr != nil {
		token = s.pairingMgr.GetToken()
	}
	return fmt.Sprintf("%s/#pair=%s&key=%s&lan=%s:%d&name=%s",
		DefaultCloudflareURL, token, s.cfg.LaptopPubKey, ip, s.port, s.cfg.DeviceName)
}

func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	// Allow Cloudflare Pages deployments
	if origin == "https://pc-remote-45t.pages.dev" || strings.HasSuffix(origin, ".pages.dev") {
		return true
	}
	// Allow localhost / loopback development
	if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
		return true
	}
	// Allow private LAN origins (192.168.x.x, 10.x.x.x, 172.16-31.x.x)
	if strings.HasPrefix(origin, "http://192.168.") || strings.HasPrefix(origin, "http://10.") || strings.HasPrefix(origin, "http://172.") {
		return true
	}
	return false
}

func (s *Server) addSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")

	origin := r.Header.Get("Origin")
	if origin != "" && isAllowedOrigin(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
	}

	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Access-Control-Request-Private-Network")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.addSecurityHeaders(w, r)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := r.URL.Path

	// Control API: strict POST only
	if path == "/api/control" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.handleControl(w, r)
		return
	}

	// Status API: sanitized minimal status (zero sensitive disclosures)
	if path == "/api/status" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "running",
			"deviceName": s.cfg.DeviceName,
		})
		return
	}

	// Pairing Display page (Desktop browser view)
	if path == "/pair" {
		s.handlePairingPage(w, r)
		return
	}

	// Static Web Assets
	if s.webFS != nil {
		cleanPath := strings.TrimPrefix(path, "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}

		// Security: Prevent serving Go source files or directory traversals
		if strings.HasSuffix(cleanPath, ".go") || strings.Contains(cleanPath, "..") {
			http.NotFound(w, r)
			return
		}

		fileData, err := fs.ReadFile(s.webFS, cleanPath)
		if err == nil {
			switch {
			case strings.HasSuffix(cleanPath, ".html"):
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
			case strings.HasSuffix(cleanPath, ".css"):
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
			case strings.HasSuffix(cleanPath, ".js"):
				w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			case strings.HasSuffix(cleanPath, ".json"):
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
			case strings.HasSuffix(cleanPath, ".svg"):
				w.Header().Set("Content-Type", "image/svg+xml")
			case strings.HasSuffix(cleanPath, ".png"):
				w.Header().Set("Content-Type", "image/png")
			}
			w.Write(fileData)
			return
		}
	}

	http.NotFound(w, r)
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 1. Bound concurrency: limit concurrent cryptographic operations to protect CPU and memory
	s.mu.RLock()
	sem := s.controlSem
	s.mu.RUnlock()

	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	default:
		// Saturated: Fail fast with 429 Too Many Requests
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Server busy: maximum concurrent control operations reached. Please retry shortly.",
		})
		return
	}

	// Security: Limit request body to 64KB to prevent DoS memory exhaustion
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to read request body"})
		return
	}

	// Require cryptographic Nostr envelope format
	var evt nostr.Event
	if err := json.Unmarshal(bodyBytes, &evt); err == nil && evt.Sig != "" && evt.Content != "" && evt.PubKey != "" {
		// Valid cryptographic envelope structure! Process through the unified security pipeline
		if s.handler == nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "Protocol handler uninitialized"})
			return
		}

		respEvt, errProc := s.handler.ProcessCommandEvent(&evt)
		if errProc != nil {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": errProc.Error()})
			return
		}

		// Return the signed, NIP-44 encrypted response Nostr event
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(respEvt)
		return
	}

	// Strictly reject legacy unencrypted or plaintext PIN requests
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "Plaintext commands rejected. PC Remote requires cryptographically signed & encrypted Nostr envelope.",
	})
}

func (s *Server) handlePairingPage(w http.ResponseWriter, r *http.Request) {
	pairingUrl := s.GetPairingURL()
	qrDataUri, _ := qr.GenerateBase64PNG(pairingUrl, 260)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>PC Remote - Pair Phone</title>
  <style>
    body {
      background: #09090b;
      color: #fafafa;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      margin: 0;
      padding: 20px;
    }
    .card {
      background: #18181b;
      border: 1px solid #27272a;
      border-radius: 16px;
      padding: 32px;
      text-align: center;
      max-width: 360px;
      width: 100%%;
      box-shadow: 0 8px 32px rgba(0,0,0,0.4);
    }
    h1 { font-size: 20px; font-weight: 600; margin: 0 0 8px 0; }
    p { color: #a1a1aa; font-size: 14px; margin: 0 0 24px 0; }
    .qr-wrap {
      background: white;
      padding: 16px;
      border-radius: 12px;
      display: inline-block;
      margin-bottom: 20px;
    }
    .qr-wrap img { display: block; width: 220px; height: 220px; }
    .device-name {
      font-size: 13px;
      color: #71717a;
      background: #27272a;
      padding: 6px 12px;
      border-radius: 20px;
      display: inline-block;
    }
  </style>
</head>
<body>
  <div class="card">
    <h1>Pair with Phone</h1>
    <p>Scan with your phone camera to pair securely</p>
    <div class="qr-wrap">
      <img src="%s" alt="Pairing QR Code">
    </div>
    <div class="device-name">%s</div>
  </div>
</body>
</html>`, qrDataUri, s.cfg.DeviceName)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

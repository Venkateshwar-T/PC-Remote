package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"laptopcontrol/internal/config"
	"laptopcontrol/internal/qr"
	"laptopcontrol/internal/win32"
)

type Server struct {
	cfg          *config.Config
	webFS        fs.FS
	port         int
	pairingToken string
	pairingExp   time.Time
	mu           sync.RWMutex
}

func NewServer(cfg *config.Config, webFS fs.FS, port int) *Server {
	s := &Server{
		cfg:   cfg,
		webFS: webFS,
		port:  port,
	}
	s.RegeneratePairingToken()
	return s
}

func (s *Server) RegeneratePairingToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := make([]byte, 16)
	rand.Read(b)
	s.pairingToken = hex.EncodeToString(b)
	s.pairingExp = time.Now().Add(5 * time.Minute)
	return s.pairingToken
}

func (s *Server) GetPairingToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pairingToken
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

// GetPairingURL generates the complete local pairing URL
func (s *Server) GetPairingURL() string {
	ip := GetLocalIPv4()
	token := s.GetPairingToken()
	return fmt.Sprintf("http://%s:%d/#pair=%s&key=%s&lan=%s:%d&name=%s",
		ip, s.port, token, s.cfg.LaptopPubKey, ip, s.port, s.cfg.DeviceName)
}

type ControlRequest struct {
	Action    string `json:"action"`
	Pin       string `json:"pin"`
	NewPin    string `json:"new_pin,omitempty"`
	Token     string `json:"token"`
	Timestamp int64  `json:"timestamp"`
}

func (s *Server) addSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Access-Control-Request-Private-Network")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.addSecurityHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := r.URL.Path

	// Control API
	if path == "/api/control" && r.Method == http.MethodPost {
		s.handleControl(w, r)
		return
	}

	// Status API
	if path == "/api/status" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "running",
			"deviceName": s.cfg.DeviceName,
			"localIp":    GetLocalIPv4(),
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

	// Security: Limit request body to 64KB to prevent DoS memory exhaustion
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

	var req ControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	// Validate PIN
	ok, errMsg := s.cfg.VerifyPin(req.Pin)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": errMsg})
		return
	}

	switch req.Action {
	case "telemetry":
		data := win32.QueryTelemetry(s.cfg.DeviceName)
		json.NewEncoder(w).Encode(data)

	case "lock":
		win32.LockScreen()
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Workstation locked"})

	case "sleep":
		win32.SuspendSystem()
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Entering sleep mode"})

	case "restart":
		win32.RebootSystem()
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Restart initiated"})

	case "shutdown":
		win32.PowerOffSystem()
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Shutdown initiated"})

	case "change_pin":
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "For security, Master PIN can only be changed directly on the PC desktop.",
		})
		return

	default:
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unknown action"})
	}
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
    body { background: #09090b; color: #f4f4f5; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; }
    .card { background: #141417; border: 1px solid #27272a; border-radius: 14px; padding: 32px; max-width: 380px; text-align: center; }
    h2 { margin: 0 0 8px 0; font-size: 20px; }
    p { color: #a1a1aa; font-size: 13px; line-height: 1.5; margin: 0 0 20px 0; }
    .qr-box { background: #ffffff; padding: 16px; border-radius: 12px; display: inline-block; margin-bottom: 20px; }
    .qr-box img { display: block; width: 220px; height: 220px; }
    .status { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; font-family: monospace; background: rgba(34,197,94,0.1); color: #22c55e; border: 1px solid rgba(34,197,94,0.25); padding: 5px 12px; border-radius: 6px; }
    .dot { width: 6px; height: 6px; border-radius: 50%%; background: #22c55e; }
  </style>
</head>
<body>
  <div class="card">
    <h2>Pair Your Phone</h2>
    <p>Point your phone's camera at the QR code below while connected to this Wi-Fi network.</p>
    <div class="qr-box">
      <img src="%s" alt="Pairing QR Code">
    </div>
    <div>
      <div class="status"><span class="dot"></span>Ready for pairing</div>
    </div>
  </div>
</body>
</html>`, qrDataUri)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func isNumericPin(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

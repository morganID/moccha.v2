package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"moccha/internal/filemanager"
	"moccha/internal/handler"
	appMiddleware "moccha/internal/mw"
	"moccha/internal/system"
	"moccha/internal/terminal"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

//go:embed web/dist
var webFiles embed.FS

//go:embed cloudflared/*
var cloudflaredBinary embed.FS

type Config struct {
	Port             string
	AuthToken        string
	RateLimit        int
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	EnableNgrok      bool
	NgrokToken       string
	EnableCloudflare bool
	CloudflareToken  string
	DebugMode        bool
	AnonMode         bool
}

var cloudflaredProcess *exec.Cmd

func main() {
	cfg := &Config{
		Port:             getEnv("PORT", "3000"),
		AuthToken:        getEnv("AUTH_TOKEN", "moccha-secret-token"),
		RateLimit:        100,
		ReadTimeout:      30 * time.Second,
		WriteTimeout:     30 * time.Second,
		IdleTimeout:      120 * time.Second,
		EnableNgrok:      false,
		NgrokToken:       "",
		EnableCloudflare: false,
		CloudflareToken:  "",
		DebugMode:        false,
		AnonMode:         false,
	}

	flag.StringVar(&cfg.Port, "port", cfg.Port, "Server port")
	flag.StringVar(&cfg.AuthToken, "token", cfg.AuthToken, "Authentication token")
	flag.BoolVar(&cfg.EnableNgrok, "ngrok", false, "Enable ngrok tunneling (deprecated, use -cloudflare)")
	flag.StringVar(&cfg.NgrokToken, "ngrok-token", cfg.NgrokToken, "Ngrok auth token (optional)")
	flag.BoolVar(&cfg.EnableCloudflare, "cloudflare", false, "Enable Cloudflare Tunnel")
	flag.StringVar(&cfg.CloudflareToken, "cloudflare-token", cfg.CloudflareToken, "Cloudflare Tunnel token")
	flag.BoolVar(&cfg.DebugMode, "debug", false, "Enable debug mode with verbose logging")
	flag.BoolVar(&cfg.AnonMode, "anon", false, "Enable anonymous mode (no authentication required)")
	flag.Parse()

	if cfg.DebugMode {
		log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
		log.Println("[DEBUG] Debug mode enabled")
	}

	termManager := terminal.New()
	sysInfo := system.New()
	fileMgr := filemanager.New()

	authMdw := appMiddleware.NewAuth(cfg.AuthToken)
	rateMdw := appMiddleware.NewRateLimiter(cfg.RateLimit)

	h := handler.New(termManager, sysInfo, fileMgr)

	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	if cfg.DebugMode {
		r.Use(debugLogger)
	} else {
		r.Use(chiMiddleware.Logger)
	}
	r.Use(chiMiddleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/", h.Index)
	r.Handle("/web/*", http.StripPrefix("/web", http.FileServer(http.FS(webFiles))))

	r.Group(func(r chi.Router) {
		// Only use auth if not in anon mode
		if !cfg.AnonMode {
			r.Use(authMdw.Authenticate)
		}
		r.Use(rateMdw.Limit)

		r.Get("/api/health", h.Health)

		r.Get("/api/system/info", h.SystemInfo)
		r.Get("/api/system/processes", h.Processes)
		r.Get("/api/system/network", h.NetworkInfo)
		r.Get("/api/system/disk", h.DiskInfo)

		r.Get("/api/files/*", h.ListFiles)
		r.Post("/api/files/*", h.CreateFile)
		r.Put("/api/files/*", h.RenameFile)
		r.Delete("/api/files/*", h.DeleteFile)
		r.Post("/api/files/upload/*", h.UploadFile)
		r.Get("/api/files/download/*", h.DownloadFile)

		r.Get("/api/terminal/ws", h.TerminalWS)
	})

	srv := &http.Server{
		Addr:         "127.0.0.1:" + cfg.Port,
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		// Try to find available port if the requested port is in use
		originalPort := cfg.Port
		if ln, err := net.Listen("tcp", "0.0.0.0:"+cfg.Port); err != nil {
			// Port is in use, find available port
			newPort, findErr := findAvailablePort(cfg.Port)
			if findErr != nil {
				log.Fatalf("Failed to find available port: %v", findErr)
			}
			cfg.Port = newPort
			srv.Addr = "0.0.0.0:" + newPort
			if cfg.DebugMode {
				log.Printf("[DEBUG] Port %s is in use, using port %s instead", originalPort, newPort)
			}
		} else {
			ln.Close()
		}

		log.Printf("Moccha server starting on port %s", cfg.Port)
		log.Printf("Auth token: %s", cfg.AuthToken)
		log.Printf("Web UI: http://localhost:%s/", cfg.Port)

		// Wait for server to start before connecting tunnel
		time.Sleep(2 * time.Second)

		// Start tunnel after port is determined
		if cfg.EnableCloudflare {
			if err := startCloudflare(cfg.Port, cfg.CloudflareToken, cfg.DebugMode); err != nil {
				log.Printf("Error: Failed to start Cloudflare Tunnel: %v", err)
				log.Println("Server will not start without tunnel. Exiting...")
				return
			}
		} else if cfg.EnableNgrok {
			if err := startNgrok(cfg.Port, cfg.NgrokToken, cfg.DebugMode); err != nil {
				log.Printf("Error: Failed to start ngrok: %v", err)
				log.Println("Server will not start without tunnel. Exiting...")
				return
			}
		}

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// First stop cloudflared/ngrok tunnel
	if cloudflaredProcess != nil && cloudflaredProcess.Process != nil {
		log.Println("Stopping Cloudflare Tunnel/ngrok...")
		// Try graceful shutdown first
		cloudflaredProcess.Process.Signal(syscall.SIGTERM)

		// Wait for process to exit with timeout
		done := make(chan struct{})
		go func() {
			cloudflaredProcess.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Println("Tunnel process exited gracefully")
		case <-time.After(5 * time.Second):
			// Force kill if still running - kill entire process group
			log.Println("Forcing tunnel process to stop...")
			syscall.Kill(-cloudflaredProcess.Process.Pid, syscall.SIGKILL)
			cloudflaredProcess.Wait()
		}
	}

	// Close all terminal sessions
	log.Println("Closing terminal sessions...")
	termManager.CloseAll()

	// Shutdown HTTP server (closes port)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}

func startNgrok(port, token string, debug bool) error {
	log.Println("Starting ngrok tunnel...")

	ngrokPath := "./cmd/server/ngrok"

	// Check if ngrok exists
	if _, err := os.Stat(ngrokPath); os.IsNotExist(err) {
		return fmt.Errorf("ngrok binary not found at %s. Run 'make embed-ngrok' or download ngrok manually", ngrokPath)
	}

	args := []string{"http", port, "--log=stdout"}
	if token != "" {
		args = append(args, "--authtoken", token)
	}

	if debug {
		log.Printf("[DEBUG] Ngrok command: %s %v", ngrokPath, args)
	}

	cmd := exec.Command(ngrokPath, args...)
	// Run ngrok in background - hide all output, use API to get URL
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ngrok: %w", err)
	}

	cloudflaredProcess = cmd

	if debug {
		log.Printf("[DEBUG] Ngrok process started with PID: %d", cmd.Process.Pid)
	}

	// Try to get the URL from ngrok API
	go func() {
		for i := 0; i < 10; i++ {
			time.Sleep(2 * time.Second)
			// Try to get URL from ngrok API
			if resp, err := http.Get("http://127.0.0.1:4040/api/tunnels"); err == nil {
				var result map[string]interface{}
				if body, err := io.ReadAll(resp.Body); err == nil {
					if json.Unmarshal(body, &result); err == nil {
						if tunnels, ok := result["tunnels"].([]interface{}); ok && len(tunnels) > 0 {
							if tunnel, ok := tunnels[0].(map[string]interface{}); ok {
								if url, ok := tunnel["public_url"].(string); ok {
									log.Printf("🌐 Ngrok URL: %s", url)
									return
								}
							}
						}
					}
				}
				resp.Body.Close()
			}
		}
		log.Println("Waiting for ngrok tunnel...")
	}()

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("Ngrok process exited: %v", err)
		}
	}()

	return nil
}

func getNgrokUrl() (string, error) {
	time.Sleep(2 * time.Second)

	data, err := os.ReadFile("/tmp/ngrok-api-url")
	if err == nil {
		return string(data), nil
	}

	return "", fmt.Errorf("ngrok URL not available")
}

func startCloudflare(port, token string, debug bool) error {
	log.Println("Starting Cloudflare Tunnel...")

	cloudflaredPath := "/tmp/moccha-cloudflared"

	data, err := cloudflaredBinary.ReadFile("cloudflared/cloudflared")
	if err != nil {
		return fmt.Errorf("cloudflared binary not embedded. Run 'make embed-cloudflare' first, or download cloudflared manually: %w", err)
	}

	if debug {
		log.Printf("[DEBUG] Writing cloudflared binary to %s (size: %d bytes)", cloudflaredPath, len(data))
	}

	if err := os.WriteFile(cloudflaredPath, data, 0755); err != nil {
		return fmt.Errorf("failed to write cloudflared binary: %w", err)
	}

	// Cloudflare Tunnel requires a token for authenticated tunnels
	// If no token provided, use quick tunnel mode
	var args []string
	if token != "" {
		// Use token-based tunnel (requires cloudflared tunnel create/run first)
		// Bind to 127.0.0.1 for reliable Cloudflare tunnel connection
		args = []string{"tunnel", "--url", "http://127.0.0.1:" + port}
		log.Println("Note: For persistent tunnels, configure a Cloudflare Tunnel manually")
	} else {
		// Use quick tunnel mode (no authentication needed, temporary URL)
		// Bind to 127.0.0.1 for reliable Cloudflare tunnel connection
		args = []string{"tunnel", "--url", "http://127.0.0.1:" + port}
	}

	if debug {
		log.Printf("[DEBUG] Cloudflare command: %s %v", cloudflaredPath, args)
	}

	cmd := exec.Command(cloudflaredPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	cloudflaredProcess = cmd

	if debug {
		log.Printf("[DEBUG] Cloudflare process started with PID: %d", cmd.Process.Pid)
	}

	go func() {
		time.Sleep(3 * time.Second)
		log.Println("Waiting for Cloudflare Tunnel...")
	}()

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("Cloudflare process exited: %v", err)
		}
	}()

	return nil
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Requested-With")
		w.Header().Set("Access-Control-Expose-Headers", "Authorization")
		w.Header().Set("Access-Control-Max-Age", "300")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func findAvailablePort(startPort string) (string, error) {
	port := startPort
	for i := 0; i < 100; i++ {
		ln, err := net.Listen("tcp", "0.0.0.0:"+port)
		if err == nil {
			ln.Close()
			return port, nil
		}
		// Try next port
		portInt := 0
		fmt.Sscanf(port, "%d", &portInt)
		if portInt == 0 {
			portInt = 8080
		}
		port = fmt.Sprintf("%d", portInt+1)
	}
	return "", fmt.Errorf("no available port found")
}

func debugLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

		log.Printf("[DEBUG] Incoming request: %s %s", r.Method, r.URL.Path)
		log.Printf("[DEBUG] Remote addr: %s", r.RemoteAddr)
		log.Printf("[DEBUG] Headers: %v", r.Header)

		start := time.Now()
		next.ServeHTTP(ww, r)
		duration := time.Since(start)

		log.Printf("[DEBUG] Response: status=%d duration=%v", ww.Status(), duration)
	})
}

func init() {
	log.SetFlags(0)
	log.Println("MOCCHA SERVER v1.0 - Remote System Management")
}

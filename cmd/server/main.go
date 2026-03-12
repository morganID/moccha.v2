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
	"runtime"
	"syscall"
	"time"

	"moccha/internal/filemanager"
	"moccha/internal/handler"
	"moccha/internal/system"
	"moccha/internal/terminal"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

// Init function untuk setup awal
func init() {
	// Check dan set umask
	syscall.Umask(0)

	// Check running user
	if os.Getuid() == 0 {
		log.Println("⚠️  WARNING: Running as root is not recommended")
	}

	// Check SHELL environment
	if os.Getenv("SHELL") == "" {
		os.Setenv("SHELL", "/bin/bash")
	}

	log.Printf("Running as user: %s (UID: %d)", os.Getenv("USER"), os.Getuid())

	// Setup logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Diagnostic info
	log.Printf("=== Moccha Starting ===")
	log.Printf("OS: %s", runtime.GOOS)
	log.Printf("Arch: %s", runtime.GOARCH)
	log.Printf("Go Version: %s", runtime.Version())
	log.Printf("User: %s (UID: %d)", os.Getenv("USER"), os.Getuid())
	log.Printf("Home: %s", os.Getenv("HOME"))
	log.Printf("PWD: %s", os.Getenv("PWD"))
	log.Printf("SHELL: %s", os.Getenv("SHELL"))

	// Check PTY availability
	if _, err := os.Stat("/dev/ptmx"); err != nil {
		log.Printf("⚠️  /dev/ptmx not accessible: %v", err)
	} else {
		log.Printf("✅ /dev/ptmx accessible")
	}
}

//go:embed web/dist
var webFiles embed.FS

//go:embed ngrok/darwin-arm64
var ngrokDarwin embed.FS

//go:embed ngrok/linux-amd64
var ngrokLinux embed.FS

type Config struct {
	Port         string
	AuthToken    string
	RateLimit    int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	EnableNgrok  bool
	NgrokToken   string
	DebugMode    bool
	AnonMode     bool
}

var ngrokProcess *exec.Cmd

func main() {
	cfg := &Config{
		Port:         getEnv("PORT", "3000"),
		AuthToken:    getEnv("AUTH_TOKEN", "moccha-secret-token"),
		RateLimit:    100,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		EnableNgrok:  true,
		NgrokToken:   "3ApnBf8MFlCNR00uqBLaBETlmgL_5aPFjn2CuMEQWxF3UanFJ",
		DebugMode:    false,
		AnonMode:     false,
	}

	flag.StringVar(&cfg.Port, "port", cfg.Port, "Server port")
	flag.StringVar(&cfg.AuthToken, "token", cfg.AuthToken, "Authentication token")
	flag.BoolVar(&cfg.EnableNgrok, "ngrok", false, "Enable ngrok tunneling")
	flag.StringVar(&cfg.NgrokToken, "ngrok-token", cfg.NgrokToken, "Ngrok auth token (optional)")
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

	h := handler.New(termManager, sysInfo, fileMgr, cfg.AuthToken)

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

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		index, err := webFiles.ReadFile("web/dist/index.html")
		if err != nil {
			http.Error(w, "Web UI not found", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(index)
	})
	r.Handle("/web/*", http.StripPrefix("/web", http.FileServer(http.FS(webFiles))))

	// Login endpoint - no auth required
	r.Post("/api/login", h.Login)

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

	srv := &http.Server{
		Addr:         "0.0.0.0:" + cfg.Port,
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
		// Use background context to prevent cancellation
		BaseContext: func(net.Listener) context.Context {
			return context.Background()
		},
	}

	// Find available port BEFORE starting server
	ln, err := net.Listen("tcp", "0.0.0.0:"+cfg.Port)
	if err != nil {
		// Port is in use, find available port
		newPort, findErr := findAvailablePort(cfg.Port)
		if findErr != nil {
			log.Fatalf("Failed to find available port: %v", findErr)
		}
		cfg.Port = newPort
		srv.Addr = "0.0.0.0:" + newPort
		if cfg.DebugMode {
			log.Printf("[DEBUG] Port was in use, using port %s instead", newPort)
		}
	} else {
		ln.Close()
	}

	log.Printf("Moccha server starting on port %s", cfg.Port)
	log.Printf("Auth token: %s", cfg.AuthToken)
	log.Printf("Web UI: http://localhost:%s/", cfg.Port)

	// Start HTTP server
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for server to be ready before starting tunnel
	log.Println("Waiting for server to be ready...")
	if err := waitForServerReady(cfg.Port, 15, 500*time.Millisecond); err != nil {
		log.Printf("Warning: Server may not be ready: %v", err)
	} else {
		log.Println("Server is ready")
	}

	// Start tunnel AFTER server is ready
	if cfg.EnableNgrok {
		if err := startNgrok(cfg.Port, cfg.NgrokToken, cfg.DebugMode); err != nil {
			log.Printf("Warning: Failed to start ngrok: %v", err)
			log.Println("Continuing without tunnel...")
		}
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// First stop ngrok tunnel
	if ngrokProcess != nil && ngrokProcess.Process != nil {
		log.Println("Stopping ngrok tunnel...")
		// Try graceful shutdown first
		ngrokProcess.Process.Signal(syscall.SIGTERM)

		// Wait for process to exit with timeout
		done := make(chan struct{})
		go func() {
			ngrokProcess.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Println("Tunnel process exited gracefully")
		case <-time.After(5 * time.Second):
			// Force kill if still running - kill entire process group
			log.Println("Forcing tunnel process to stop...")
			syscall.Kill(-ngrokProcess.Process.Pid, syscall.SIGKILL)
			ngrokProcess.Wait()
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

	runtimeOS := runtime.GOOS
	runtimeArch := runtime.GOARCH

	var ngrokPath string
	var data []byte

	if runtimeOS == "darwin" && runtimeArch == "arm64" {
		ngrokPath = "./cmd/server/ngrok/darwin-arm64/ngrok"
		if _, statErr := os.Stat(ngrokPath); os.IsNotExist(statErr) {
			var readErr error
			data, readErr = ngrokDarwin.ReadFile("ngrok/darwin-arm64/ngrok")
			if readErr != nil {
				return fmt.Errorf("ngrok darwin-arm64 binary not found: %w", readErr)
			}
		}
	} else if runtimeOS == "linux" && runtimeArch == "amd64" {
		ngrokPath = "./cmd/server/ngrok/linux-amd64/ngrok"
		if _, statErr := os.Stat(ngrokPath); os.IsNotExist(statErr) {
			var readErr error
			data, readErr = ngrokLinux.ReadFile("ngrok/linux-amd64/ngrok")
			if readErr != nil {
				return fmt.Errorf("ngrok linux-amd64 binary not found: %w", readErr)
			}
		}
	} else {
		return fmt.Errorf("unsupported platform: %s/%s", runtimeOS, runtimeArch)
	}

	if data != nil {
		ngrokPath = "/tmp/moccha-ngrok"
		if debug {
			log.Printf("[DEBUG] Writing ngrok binary to %s (size: %d bytes)", ngrokPath, len(data))
		}
		if err := os.WriteFile(ngrokPath, data, 0755); err != nil {
			return fmt.Errorf("failed to write ngrok binary: %w", err)
		}
	} else if debug {
		log.Printf("[DEBUG] Using existing ngrok binary at %s", ngrokPath)
	}

	args := []string{"http", port, "--log=stdout", "--domain=sombrous-villose-nidia.ngrok-free.dev"}
	if token != "" {
		args = append(args, "--authtoken", token)
	}

	if debug {
		log.Printf("[DEBUG] Ngrok command: %s %v", ngrokPath, args)
	}

	cmd := exec.Command(ngrokPath, args...)
	// Run ngrok in background - show output for debugging
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ngrok: %w", err)
	}

	ngrokProcess = cmd

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

func waitForServerReady(port string, maxRetries int, delay time.Duration) error {
	for i := 0; i < maxRetries; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 1*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(delay)
	}
	return fmt.Errorf("server not ready after %d retries", maxRetries)
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

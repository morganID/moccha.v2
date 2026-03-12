package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
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

//go:embed ngrok
var ngrokBinary embed.FS

type Config struct {
	Port         string
	AuthToken    string
	RateLimit    int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	EnableNgrok  bool
	NgrokToken   string
}

var ngrokProcess *exec.Cmd

func main() {
	cfg := &Config{
		Port:         getEnv("PORT", "8080"),
		AuthToken:    getEnv("AUTH_TOKEN", "moccha-secret-token"),
		RateLimit:    100,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		EnableNgrok:  false,
		NgrokToken:   "",
	}

	flag.StringVar(&cfg.Port, "port", cfg.Port, "Server port")
	flag.StringVar(&cfg.AuthToken, "token", cfg.AuthToken, "Authentication token")
	flag.BoolVar(&cfg.EnableNgrok, "ngrok", false, "Enable ngrok tunneling")
	flag.StringVar(&cfg.NgrokToken, "ngrok-token", cfg.NgrokToken, "Ngrok auth token (optional)")
	flag.Parse()

	termManager := terminal.New()
	sysInfo := system.New()
	fileMgr := filemanager.New()

	authMdw := appMiddleware.NewAuth(cfg.AuthToken)
	rateMdw := appMiddleware.NewRateLimiter(cfg.RateLimit)

	h := handler.New(termManager, sysInfo, fileMgr)

	r := chi.NewRouter()
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/", h.Index)
	r.Handle("/web/*", http.StripPrefix("/web", http.FileServer(http.FS(webFiles))))

	r.Group(func(r chi.Router) {
		r.Use(authMdw.Authenticate)
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
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		log.Printf("Moccha server starting on port %s", cfg.Port)
		log.Printf("Auth token: %s", cfg.AuthToken)
		log.Printf("Web UI: http://localhost:%s/", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	if cfg.EnableNgrok {
		if err := startNgrok(cfg.Port, cfg.NgrokToken); err != nil {
			log.Printf("Warning: Failed to start ngrok: %v", err)
		}
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if ngrokProcess != nil {
		ngrokProcess.Process.Kill()
	}

	termManager.CloseAll()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}

func startNgrok(port, token string) error {
	log.Println("Starting ngrok tunnel...")

	ngrokPath := "/tmp/moccha-ngrok"

	data, err := ngrokBinary.ReadFile("ngrok")
	if err != nil {
		return fmt.Errorf("ngrok binary not embedded. Run 'make embed-ngrok' first, or download ngrok manually: %w", err)
	}

	if err := os.WriteFile(ngrokPath, data, 0755); err != nil {
		return fmt.Errorf("failed to write ngrok binary: %w", err)
	}

	args := []string{"http", "--region", "us", port}
	if token != "" {
		args = append(args, "--authtoken", token)
	}

	cmd := exec.Command(ngrokPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ngrok: %w", err)
	}

	ngrokProcess = cmd

	go func() {
		time.Sleep(3 * time.Second)
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

func init() {
	fmt.Println(`
╔═══════════════════════════════════════════╗
║           MOCCHA SERVER v1.0              ║
║    Remote System Management for Colab      ║
║           + Ngrok Integration             ║
╚═══════════════════════════════════════════╝
`)
}

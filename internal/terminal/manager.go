package terminal

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

type Session struct {
	ID       string
	PTY      *os.File
	WS       *websocket.Conn
	Cmd      *exec.Cmd
	closed   int32 // atomic flag
	removing int32 // atomic flag
	mu       sync.Mutex
	done     chan struct{} // untuk koordinasi goroutine
}

type Message struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	EnableCompression: true,
}

const (
	pingInterval = 25 * time.Second
	writeTimeout = 10 * time.Second
	readTimeout  = 60 * time.Second
)

func New() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) CreateSession(id string, ws *websocket.Conn, cols, rows int) (*Session, error) {
	shell := "/bin/bash"
	if _, err := os.Stat("/bin/bash"); err != nil {
		return nil, fmt.Errorf("no shell found")
	}

	log.Printf("[Terminal] Starting shell: %s", shell)

	cmd := exec.Command(shell, "--login")
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
	)

	// Set working directory to home
	if homeDir, err := os.UserHomeDir(); err == nil {
		cmd.Dir = homeDir
	}

	// Platform-specific setup
	setupProcessAttributes(cmd)

	pt, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[Terminal] PTY Start error: %v", err)
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	log.Printf("[Terminal] PTY started successfully, cols=%d rows=%d", cols, rows)

	if err := pty.Setsize(pt, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}); err != nil {
		log.Printf("Failed to set PTY size: %v", err)
	}

	session := &Session{
		ID:   id,
		PTY:  pt,
		WS:   ws,
		Cmd:  cmd,
		done: make(chan struct{}),
	}

	m.mu.Lock()
	m.sessions[id] = session
	m.mu.Unlock()

	// Setup pong handler untuk ping/pong
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	go m.handlePtyToWs(session)
	go m.handleWsToPty(session)
	go m.sendPing(session)

	return session, nil
}

func (m *Manager) handlePtyToWs(s *Session) {
	defer m.cleanup(s)

	buf := make([]byte, 8192) // Buffer lebih besar untuk performance
	for {
		select {
		case <-s.done:
			return
		default:
		}

		// Set read deadline pada PTY
		s.PTY.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, err := s.PTY.Read(buf)
		if err != nil {
			if err == io.EOF || os.IsTimeout(err) {
				continue
			}
			log.Printf("[Terminal] PTY read error for session %s: %v", s.ID, err)
			return
		}

		if n == 0 {
			continue
		}

		// Non-blocking check untuk closed state
		if atomic.LoadInt32(&s.closed) == 1 {
			return
		}

		s.mu.Lock()
		s.WS.SetWriteDeadline(time.Now().Add(writeTimeout))
		err = s.WS.WriteMessage(websocket.BinaryMessage, buf[:n])
		s.mu.Unlock()

		if err != nil {
			log.Printf("[Terminal] WebSocket write error for session %s: %v", s.ID, err)
			return
		}
	}
}

func (m *Manager) sendPing(s *Session) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			// Check atomic flag tanpa lock
			if atomic.LoadInt32(&s.closed) == 1 {
				return
			}

			s.mu.Lock()
			s.WS.SetWriteDeadline(time.Now().Add(writeTimeout))
			err := s.WS.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeTimeout))
			s.mu.Unlock()

			if err != nil {
				log.Printf("[Terminal] WebSocket ping error for session %s: %v", s.ID, err)
				m.RemoveSession(s.ID)
				return
			}
		}
	}
}

func (m *Manager) handleWsToPty(s *Session) {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		s.WS.SetReadDeadline(time.Now().Add(readTimeout))
		msgType, msg, err := s.WS.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				log.Printf("[Terminal] WebSocket unexpected close for session %s: %v", s.ID, err)
			}
			return
		}

		if atomic.LoadInt32(&s.closed) == 1 {
			return
		}

		switch msgType {
		case websocket.BinaryMessage:
			// Write dengan timeout
			log.Printf("[Terminal] Received binary message, length: %d", len(msg))
			s.PTY.SetWriteDeadline(time.Now().Add(writeTimeout))
			if _, err := s.PTY.Write(msg); err != nil {
				log.Printf("[Terminal] PTY write error: %v", err)
				return
			}

		case websocket.TextMessage:
			log.Printf("[Terminal] Received text message: %s", string(msg))
			// Check if it's JSON or plain text
			if len(msg) > 0 && msg[0] == '{' {
				// Proper JSON parsing
				var m Message
				if err := json.Unmarshal(msg, &m); err != nil {
					log.Printf("[Terminal] JSON parse error: %v", err)
					continue
				}

				switch m.Type {
				case "resize":
					if m.Cols > 0 && m.Rows > 0 && m.Cols < 1000 && m.Rows < 1000 {
						if err := pty.Setsize(s.PTY, &pty.Winsize{
							Cols: uint16(m.Cols),
							Rows: uint16(m.Rows),
						}); err != nil {
							log.Printf("[Terminal] Resize error: %v", err)
						}
					}

				case "ping":
					s.mu.Lock()
					s.WS.SetWriteDeadline(time.Now().Add(writeTimeout))
					s.WS.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
					s.mu.Unlock()
				}
			} else {
				// Plain text (keystrokes) - write directly to PTY
				log.Printf("[Terminal] Plain text (keystrokes): %s", string(msg))
				s.PTY.SetWriteDeadline(time.Now().Add(writeTimeout))
				if _, err := s.PTY.Write(msg); err != nil {
					log.Printf("[Terminal] PTY write error: %v", err)
					return
				}
			}
		}
	}
}

func (m *Manager) cleanup(s *Session) {
	// Hanya cleanup sekali
	if !atomic.CompareAndSwapInt32(&s.closed, 0, 1) {
		return
	}

	close(s.done)

	// Close PTY
	if s.PTY != nil {
		s.PTY.Close()
	}

	// Close WebSocket
	s.mu.Lock()
	if s.WS != nil {
		s.WS.Close()
	}
	s.mu.Unlock()

	// Kill process group
	if s.Cmd != nil && s.Cmd.Process != nil {
		killProcessTree(s.Cmd.Process.Pid)
		s.Cmd.Wait()
	}

	// Remove dari manager
	m.mu.Lock()
	delete(m.sessions, s.ID)
	m.mu.Unlock()

	log.Printf("[Terminal] Session %s cleaned up", s.ID)
}

func (m *Manager) Resize(id string, cols, rows int) error {
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session not found")
	}

	if atomic.LoadInt32(&session.closed) == 1 {
		return fmt.Errorf("session is closed")
	}

	return pty.Setsize(session.PTY, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func (m *Manager) RemoveSession(id string) {
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return
	}

	m.cleanup(session)
}

func (m *Manager) CloseAll() {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()

	// Cleanup semua session
	for _, s := range sessions {
		m.cleanup(s)
	}
}

func (m *Manager) GetSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// setupProcessAttributes sets platform-specific process attributes
func setupProcessAttributes(cmd *exec.Cmd) {
	// Only set on Unix systems, but be careful with Setpgid
	// It can cause "operation not permitted" errors on some systems
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: false, // Disable setpgid to avoid permission issues
		}
	}
}

// killProcessTree kills a process and all its children
func killProcessTree(pid int) error {
	if runtime.GOOS == "windows" {
		return exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(pid)).Run()
	}

	// Get process group ID
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return err
	}

	// Kill the process group
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		return err
	}

	// Wait a bit for graceful shutdown
	time.Sleep(100 * time.Millisecond)

	// Force kill if still alive
	syscall.Kill(-pgid, syscall.SIGKILL)

	return nil
}

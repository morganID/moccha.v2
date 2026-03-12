package terminal

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

type Session struct {
	ID     string
	PTY    *os.File
	WS     *websocket.Conn
	Closed bool
	Cmd    *exec.Cmd
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
}

func New() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) CreateSession(id string, ws *websocket.Conn, cols, rows int) (*Session, error) {
	shell := "/bin/bash"
	if _, err := os.Stat("/bin/zsh"); err == nil {
		shell = "/bin/zsh"
	}

	cmd := exec.Command(shell, []string{"--login"}...)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
	)

	pt, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to start PTY: %w", err)
	}

	if err := pty.Setsize(pt, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	}); err != nil {
		log.Printf("Failed to set PTY size: %v", err)
	}

	session := &Session{
		ID:  id,
		PTY: pt,
		WS:  ws,
		Cmd: cmd,
	}

	m.mu.Lock()
	m.sessions[id] = session
	m.mu.Unlock()

	go m.handlePtyToWs(session)
	go m.handleWsToPty(session)

	return session, nil
}

func (m *Manager) handlePtyToWs(s *Session) {
	defer func() {
		s.WS.Close()
		m.RemoveSession(s.ID)
	}()

	buf := make([]byte, 1024)
	for {
		n, err := s.PTY.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("PTY read error: %v", err)
			break
		}

		err = s.WS.WriteMessage(websocket.BinaryMessage, buf[:n])
		if err != nil {
			break
		}
	}
}

func (m *Manager) handleWsToPty(s *Session) {
	defer func() {
		s.WS.Close()
		m.RemoveSession(s.ID)
	}()

	for {
		msgType, msg, err := s.WS.ReadMessage()
		if err != nil {
			break
		}

		if msgType == websocket.BinaryMessage {
			s.PTY.Write(msg)
		} else if msgType == websocket.TextMessage {
			var m Message
			if err := parseMessage(msg, &m); err != nil {
				continue
			}

			switch m.Type {
			case "resize":
				if m.Cols > 0 && m.Rows > 0 {
					pty.Setsize(s.PTY, &pty.Winsize{
						Cols: uint16(m.Cols),
						Rows: uint16(m.Rows),
					})
				}
			case "ping":
				s.WS.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
			}
		}
	}
}

func parseMessage(data []byte, m *Message) error {
	if len(data) > 0 && data[0] == '{' {
		if data[1] == '"' {
			return nil
		}
	}
	return nil
}

func (m *Manager) Resize(id string, cols, rows int) error {
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session not found")
	}

	return pty.Setsize(session.PTY, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func (m *Manager) RemoveSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[id]; ok {
		if !session.Closed {
			session.Closed = true
			session.PTY.Close()
			if session.Cmd != nil {
				session.Cmd.Process.Kill()
				session.Cmd.Wait()
			}
		}
		delete(m.sessions, id)
	}
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, session := range m.sessions {
		if !session.Closed {
			session.Closed = true
			session.PTY.Close()
			if session.Cmd != nil {
				session.Cmd.Process.Kill()
				session.Cmd.Wait()
			}
		}
	}
	m.sessions = make(map[string]*Session)
}

func (m *Manager) GetSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func init() {
	_ = time.Second
}

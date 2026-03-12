# Moccha

Remote System Management - A Go-based web terminal and system management tool with ngrok integration.

## Features

- 🌐 **Web Terminal** - Interactive terminal accessible via browser
- 📊 **System Monitoring** - CPU, Memory, Disk, Network stats
- 📁 **File Manager** - Upload, download, create, delete files
- 🔗 **ngrok Integration** - Expose local server to internet
- 🔐 **Authentication** - Token-based authentication

## Quick Start

```bash
# Download from releases
wget https://github.com/morganID/moccha.v2/releases/latest/download/moccha-linux-amd64
chmod +x moccha-linux-amd64

# Run with ngrok
./moccha-linux-amd64 -ngrok -port 8080 -token yoursecret

# Or without ngrok (local only)
./moccha-linux-amd64 -port 8080 -token yoursecret
```

## Command Line Options

| Flag | Description | Default |
|------|-------------|---------|
| `-port` | Server port | `8080` |
| `-token` | Authentication token | `mysecret` |
| `-ngrok` | Enable ngrok tunneling | `false` |
| `-ngrok-token` | Ngrok auth token | (embedded) |
| `-debug` | Enable debug logging | `false` |
| `-anon` | Anonymous mode (no auth) | `false` |

## API Endpoints

### Authentication
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/login` | Authenticate with token |

### Health & System Info
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Health check |
| GET | `/api/system/info` | System information (CPU, Memory, OS) |
| GET | `/api/system/processes` | Running processes |
| GET | `/api/system/network` | Network interfaces |
| GET | `/api/system/disk` | Disk usage |

### File Manager
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/files/*` | List files/directories |
| POST | `/api/files/*` | Create file or directory |
| PUT | `/api/files/*` | Rename file/directory |
| DELETE | `/api/files/*` | Delete file/directory |
| POST | `/api/files/upload/*` | Upload file |
| GET | `/api/files/download/*` | Download file |

### Terminal
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/terminal/ws` | WebSocket terminal |

### Web UI
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/` | Web UI (HTML) |
| GET | `/web/*` | Static assets |

## Usage Examples

### Google Colab
```python
# Download binary
!wget -O moccha https://github.com/morganID/moccha.v2/releases/download/v2.0.1/moccha-linux-amd64
!chmod +x moccha

# Run with ngrok
!./moccha -ngrok -port 8080 -token colabsecret
```

### Docker
```dockerfile
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
COPY moccha-linux-amd64 /moccha
RUN chmod +x /moccha
EXPOSE 8080
CMD ["/moccha", "-port", "8080", "-token", "secret"]
```

## Development

```bash
# Build for current platform
make build

# Build for all platforms
make all

# Run locally
make run

# Run with debug
make run-debug

# Run with ngrok
make run-ngrok
```

## Tech Stack

- **Go** - Backend
- **chi** - HTTP router
- **gorilla/websocket** - WebSocket
- **github.com/creack/pty** - PTY handling
- **xterm.js** - Terminal UI

## License

MIT

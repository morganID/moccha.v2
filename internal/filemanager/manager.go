package filemanager

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type FileManager struct {
	rootPath string
}

type FileInfo struct {
	Name    string      `json:"name"`
	Path    string      `json:"path"`
	IsDir   bool        `json:"isDir"`
	Size    int64       `json:"size"`
	Mode    os.FileMode `json:"mode"`
	ModTime int64       `json:"modTime"`
}

func New() *FileManager {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	return &FileManager{
		rootPath: home,
	}
}

func (fm *FileManager) resolvePath(path string) string {
	if path == "" || path == "/" {
		return fm.rootPath
	}
	cleanPath := filepath.Clean(path)
	if filepath.IsAbs(cleanPath) {
		return cleanPath
	}
	return filepath.Join(fm.rootPath, cleanPath)
}

func (fm *FileManager) List(path string) ([]FileInfo, error) {
	absPath := fm.resolvePath(path)

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	result := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		result = append(result, FileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(path, entry.Name()),
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime().Unix(),
		})
	}

	return result, nil
}

func (fm *FileManager) Create(path string, isDir bool) error {
	absPath := fm.resolvePath(path)

	if isDir {
		return os.MkdirAll(absPath, 0755)
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(absPath)
	if err != nil {
		return err
	}
	defer file.Close()

	return nil
}

func (fm *FileManager) Delete(path string) error {
	absPath := fm.resolvePath(path)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path does not exist: %w", err)
	}

	if info.IsDir() {
		return os.RemoveAll(absPath)
	}

	return os.Remove(absPath)
}

func (fm *FileManager) Rename(oldPath, newPath string) error {
	absOldPath := fm.resolvePath(oldPath)
	absNewPath := fm.resolvePath(newPath)

	return os.Rename(absOldPath, absNewPath)
}

func (fm *FileManager) Upload(path string, r io.Reader) error {
	absPath := fm.resolvePath(path)

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	file, err := os.Create(absPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, r)
	return err
}

func (fm *FileManager) Download(path string) (io.ReadCloser, error) {
	absPath := fm.resolvePath(path)

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return nil, fmt.Errorf("cannot download directory")
	}

	return os.Open(absPath)
}

func (fm *FileManager) ReadFile(path string) ([]byte, error) {
	absPath := fm.resolvePath(path)
	return os.ReadFile(absPath)
}

func (fm *FileManager) WriteFile(path string, content []byte) error {
	absPath := fm.resolvePath(path)
	dir := filepath.Dir(absPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(absPath, content, 0644)
}

func (fm *FileManager) GetRootPath() string {
	return fm.rootPath
}

func (fm *FileManager) IsValidPath(path string) bool {
	resolved := fm.resolvePath(path)
	rootResolved := fm.resolvePath("/")

	if !strings.HasPrefix(resolved, rootResolved) {
		if resolved != rootResolved {
			return false
		}
	}

	return true
}

type uploadResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		fmt.Fprint(w, data)
	}
}

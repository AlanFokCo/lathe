// Package session persists lathe conversations as JSONL transcripts under
// ~/.lathe/projects/<enc-cwd>/<id>.jsonl (claude-code-style project dirs).
package session

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
)

// Session is a persisted conversation. Path is the JSONL file path (derived,
// not stored in the metadata line — set on Load).
type Session struct {
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Path      string `json:"-"`
}

// New creates a new session for cwd+model: generates an ID and the project-dir
// JSONL path. Does not write anything (call SaveMeta + Save).
func New(cwd, model string) (*Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("session: home dir: %w", err)
	}
	id := generateID()
	dir := filepath.Join(home, ".lathe", "projects", encodeCwd(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("session: mkdir: %w", err)
	}
	return &Session{
		ID:        id,
		Cwd:       cwd,
		Model:     model,
		CreatedAt: time.Now().Format(time.RFC3339),
		Path:      filepath.Join(dir, id+".jsonl"),
	}, nil
}

// SaveMeta writes the metadata line (first line of the JSONL).
func (s *Session) SaveMeta() error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(s.Path, b, 0o644)
}

// Save appends one message as a JSON line.
func (s *Session) Save(msg *message.Msg) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(b)
	return err
}

// Load reads a session by ID (scans all project dirs for <id>.jsonl).
func Load(id string) (*Session, []*message.Msg, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	projectsDir := filepath.Join(home, ".lathe", "projects")
	var path string
	_ = filepath.Walk(projectsDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() && info.Name() == id+".jsonl" {
			path = p
		}
		return nil
	})
	if path == "" {
		return nil, nil, fmt.Errorf("session %q not found", id)
	}
	return loadFile(path)
}

// Summary is a lightweight session record for the /resume list (M6c-5).
type Summary struct {
	ID          string
	Model       string
	FirstPrompt string
	ModTime     time.Time
}

// List returns summaries of all sessions in cwd's project dir, newest first
// (by file mtime). Best-effort: returns nil when the dir is missing; corrupt
// transcripts are skipped, not fatal. /resume needs a listing that always
// succeeds so the UI can render a "no sessions" hint instead of an error.
func List(cwd string) []Summary {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".lathe", "projects", encodeCwd(cwd))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Summary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		s := summarizeFile(filepath.Join(dir, e.Name()), info.ModTime())
		if s.ID == "" {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out
}

// summarizeFile reads the metadata line and scans for the first user-role
// message with non-empty text. Returns zero Summary when the metadata line is
// missing/corrupt (caller filters those out).
func summarizeFile(path string, mtime time.Time) Summary {
	f, err := os.Open(path)
	if err != nil {
		return Summary{}
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return Summary{}
	}
	var meta Session
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
		return Summary{}
	}
	s := Summary{ID: meta.ID, Model: meta.Model, ModTime: mtime}
	for scanner.Scan() {
		var msg message.Msg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Role != message.RoleUser {
			continue
		}
		if text := firstUserText(&msg); text != "" {
			s.FirstPrompt = text
			break
		}
	}
	return s
}

// firstUserText returns the first non-empty TextBlock on a user message.
// Empty on tool-result-only user messages (which have no text blocks).
func firstUserText(msg *message.Msg) string {
	for _, b := range msg.GetContentBlocks(message.ContentBlockText) {
		if tb, ok := b.(message.TextBlock); ok {
			if t := strings.TrimSpace(tb.Text); t != "" {
				return t
			}
		}
	}
	return ""
}

// Latest returns the most recent session in cwd's project dir.
func Latest(cwd string) (*Session, []*message.Msg, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	dir := filepath.Join(home, ".lathe", "projects", encodeCwd(cwd))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("no sessions in %q", cwd)
	}
	var newest string
	var newestMtime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMtime) {
			newest = filepath.Join(dir, e.Name())
			newestMtime = info.ModTime()
		}
	}
	if newest == "" {
		return nil, nil, fmt.Errorf("no sessions in %q", cwd)
	}
	return loadFile(newest)
}

func loadFile(path string) (*Session, []*message.Msg, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	data = append(data, '\n') // ensure trailing newline for a clean split
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		return nil, nil, fmt.Errorf("session: empty file")
	}
	var sess Session
	if err := json.Unmarshal([]byte(lines[0]), &sess); err != nil {
		return nil, nil, fmt.Errorf("session: metadata: %w", err)
	}
	sess.Path = path
	var msgs []*message.Msg
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var msg message.Msg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			fmt.Fprintf(os.Stderr, "session: skipping corrupt line: %v\n", err)
			continue
		}
		msgs = append(msgs, &msg)
	}
	return &sess, msgs, nil
}

func encodeCwd(cwd string) string {
	s := strings.TrimPrefix(cwd, "/")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

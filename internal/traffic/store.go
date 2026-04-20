package traffic

import (
	"sync"
	"time"
)

// Entry represents one proxy traffic log item.
type Entry struct {
	ID            uint64 `json:"id"`
	TraceID       uint64 `json:"trace_id"`
	Timestamp     string `json:"timestamp"`
	Direction     string `json:"direction"`
	RemoteAddr    string `json:"remote_addr,omitempty"`
	Method        string `json:"method,omitempty"`
	Path          string `json:"path,omitempty"`
	Query         string `json:"query,omitempty"`
	StatusCode    int    `json:"status_code,omitempty"`
	BytesWritten  int64  `json:"bytes_written,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
	Body          string `json:"body,omitempty"`
	BodyTruncated bool   `json:"body_truncated,omitempty"`
}

// Store keeps recent traffic logs in memory.
type Store struct {
	mu      sync.RWMutex
	maxSize int
	nextID  uint64
	entries []Entry
}

func NewStore(maxSize int) *Store {
	if maxSize <= 0 {
		maxSize = 2000
	}
	return &Store{
		maxSize: maxSize,
		entries: make([]Entry, 0, maxSize),
	}
}

func (s *Store) Add(entry Entry) Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	entry.ID = s.nextID
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().Format(time.RFC3339Nano)
	}

	s.entries = append(s.entries, entry)
	if len(s.entries) > s.maxSize {
		overflow := len(s.entries) - s.maxSize
		s.entries = append([]Entry(nil), s.entries[overflow:]...)
	}

	return entry
}

func (s *Store) List(sinceID uint64, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > s.maxSize {
		limit = s.maxSize
	}

	result := make([]Entry, 0, limit)
	for i := len(s.entries) - 1; i >= 0; i-- {
		entry := s.entries[i]
		if entry.ID <= sinceID {
			break
		}
		result = append(result, entry)
		if len(result) >= limit {
			break
		}
	}

	// Reverse back to chronological order.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = s.entries[:0]
}

package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Memory is an in-process ObjectStore.
//
// It exists for two reasons. A unit test can exercise the service's authorization
// and its object/row ordering rules without a network, and a deployment that wants
// to try attachments before wiring a bucket can run against it. It is not durable
// and must never be the store behind real customer data: an object lives only as
// long as the process.
type Memory struct {
	mu      sync.Mutex
	objects map[string]memoryObject
	now     func() time.Time
	// FailPut, FailDelete and FailStat make the store fail on demand so a test can
	// drive the compensating-delete and reconciliation paths.
	FailPut    error
	FailDelete error
	FailStat   error
}

type memoryObject struct {
	data         []byte
	contentType  string
	lastModified time.Time
}

// NewMemory builds an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{objects: map[string]memoryObject{}, now: time.Now}
}

var _ ObjectStore = (*Memory)(nil)

// Enabled reports whether the store is usable.
func (m *Memory) Enabled() bool { return m != nil }

func (m *Memory) Put(_ context.Context, key string, r io.Reader, size int64, contentType string) (ObjectInfo, error) {
	if m.FailPut != nil {
		return ObjectInfo{}, m.FailPut
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("memory store: read body: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memoryObject{data: data, contentType: contentType, lastModified: m.now()}
	return ObjectInfo{
		Key:         key,
		Size:        int64(len(data)),
		ContentType: contentType,
		ETag:        fmt.Sprintf("%d", len(data)),
	}, nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.objects[key]
	if !ok {
		return nil, ObjectInfo{}, fmt.Errorf("%w: %s", ErrObjectMissing, key)
	}
	return io.NopCloser(bytes.NewReader(obj.data)), ObjectInfo{
		Key:         key,
		Size:        int64(len(obj.data)),
		ContentType: obj.contentType,
	}, nil
}

func (m *Memory) Stat(_ context.Context, key string) (ObjectInfo, error) {
	if m.FailStat != nil {
		return ObjectInfo{}, m.FailStat
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.objects[key]
	if !ok {
		return ObjectInfo{}, fmt.Errorf("%w: %s", ErrObjectMissing, key)
	}
	return ObjectInfo{Key: key, Size: int64(len(obj.data)), ContentType: obj.contentType}, nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	if m.FailDelete != nil {
		return m.FailDelete
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *Memory) PresignGet(_ context.Context, key string, ttl time.Duration) (*url.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrObjectMissing, key)
	}
	return url.Parse(fmt.Sprintf("https://memory.local/%s?expires=%d", key, int(ttl.Seconds())))
}

func (m *Memory) List(_ context.Context, prefix string) ([]ObjectMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ObjectMeta
	for k, v := range m.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, ObjectMeta{Key: k, Size: int64(len(v.data)), LastModified: v.lastModified})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// PutObject is a test helper that seeds an object directly.
func (m *Memory) PutObject(key string, data []byte, contentType string, modified time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memoryObject{data: data, contentType: contentType, lastModified: modified}
}

// SetNow overrides the clock used for LastModified, so a test can age an object
// past the reconciliation grace period without sleeping.
func (m *Memory) SetNow(fn func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = fn
}

// Len reports the number of stored objects.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}

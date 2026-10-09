package geo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func newTestStore(t *testing.T, h http.HandlerFunc) (*Store, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.endpoint = srv.URL
	return s, &calls
}

func TestRefresh(t *testing.T) {
	s, calls := newTestStore(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct{ IP string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"ip": req.IP, "country": "日本", "countryCode": "jp"},
		})
	})
	if !s.Refresh(context.Background(), []string{"203.0.113.9", "[203.0.113.9]", "10.0.0.1", "100.64.1.1"}, on) {
		t.Fatal("first refresh should report a change")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1 (dedup + private skip)", got)
	}
	if e := s.Get("203.0.113.9"); e.CountryCode != "JP" || e.Country != "日本" {
		t.Fatalf("entry = %+v", e)
	}
	if e := s.Get("10.0.0.1"); e.CountryCode != "" || e.Failed {
		t.Fatalf("private entry = %+v", e)
	}
	if s.Refresh(context.Background(), []string{"203.0.113.9"}, on) || calls.Load() != 1 {
		t.Fatal("fresh entry should not be queried again")
	}

	// 重新打开读回磁盘缓存。
	s2, err := Open(dirOf(s.path))
	if err != nil {
		t.Fatal(err)
	}
	if e := s2.Get("203.0.113.9"); e.CountryCode != "JP" {
		t.Fatalf("reloaded entry = %+v", e)
	}

	s.Prune([]string{"10.0.0.1"})
	if e := s.Get("203.0.113.9"); e.CountryCode != "" {
		t.Fatalf("pruned entry still present: %+v", e)
	}
}

func TestRefreshRateLimited(t *testing.T) {
	s, _ := newTestStore(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 429, "data": nil, "msg": "slow down", "retryAfter": 30})
	})
	s.Refresh(context.Background(), []string{"203.0.113.9"}, on)
	s.mu.Lock()
	_, cached := s.entries["203.0.113.9"]
	s.mu.Unlock()
	if cached {
		t.Fatal("rate-limited lookup must not be cached")
	}
}

func TestRefreshFailureIsShortLived(t *testing.T) {
	s, _ := newTestStore(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "data": nil, "msg": "boom"})
	})
	s.Refresh(context.Background(), []string{"203.0.113.9"}, on)
	e := s.Get("203.0.113.9")
	if !e.Failed || e.CountryCode != "" {
		t.Fatalf("entry = %+v", e)
	}
	if e.stale(time.Now()) || !e.stale(time.Now().Add(2*time.Hour)) {
		t.Fatal("failed entry should expire after failTTL")
	}
}

func on() bool { return true }

func TestRefreshStopsWhenDisabled(t *testing.T) {
	s, calls := newTestStore(t, func(w http.ResponseWriter, _ *http.Request) {})
	if s.Refresh(context.Background(), []string{"203.0.113.9"}, func() bool { return false }) || calls.Load() != 0 {
		t.Fatal("disabled refresh must not query")
	}
}

func TestClear(t *testing.T) {
	s, _ := newTestStore(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"countryCode": "US"}})
	})
	s.Refresh(context.Background(), []string{"203.0.113.9"}, on)
	s.Clear()
	if e := s.Get("203.0.113.9"); e.CountryCode != "" {
		t.Fatalf("entry after clear = %+v", e)
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatalf("cache file still exists: %v", err)
	}
}

func dirOf(path string) string { return filepath.Dir(filepath.Dir(path)) }

// Package geo 查询并缓存服务器所在国家, 列表据此显示国旗。
//
// 主机名先在本机解析成 IP, 再经自有的 ip.czl.net 查归属; 结果按主机缓存在本机
// `cache/geo/hosts.json`, 不参与同步 (同一主机在不同网络下可能解析到不同 IP)。
// 查询是旁路增强: 失败只记一次短期的负缓存, 不报错、不阻塞列表。
package geo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	queryURL = "https://ip.czl.net/api/query"
	// okTTL 是查到结果后的复查间隔; failTTL 是失败 (解析不了 / 接口出错) 后的重试间隔。
	okTTL   = 7 * 24 * time.Hour
	failTTL = time.Hour
)

var countryCodeRe = regexp.MustCompile(`^[A-Z]{2}$`)

// Entry 是一个主机的查询结果。CountryCode 为空表示没有国家可显示 (私网地址或查询失败)。
type Entry struct {
	IP          string    `json:"ip"`
	CountryCode string    `json:"countryCode"`
	Country     string    `json:"country"`
	Failed      bool      `json:"failed,omitempty"`
	CheckedAt   time.Time `json:"checkedAt"`
}

func (e Entry) stale(now time.Time) bool {
	ttl := okTTL
	if e.Failed {
		ttl = failTTL
	}
	return now.Sub(e.CheckedAt) > ttl
}

// Store 管理国家缓存。
type Store struct {
	path     string
	endpoint string
	client   *http.Client
	mu       sync.Mutex
	entries  map[string]Entry
	// running 保证同一时间只有一轮刷新, 列表频繁刷新时不会重复打接口。
	running bool
	// gen 在 Clear 时递增; 进行中的刷新发现代数变了就丢弃结果, 不把清掉的缓存写回去。
	gen int
}

// Open 读取缓存, 文件不存在或损坏时从空缓存开始。
func Open(cacheDir string) (*Store, error) {
	dir := filepath.Join(cacheDir, "geo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("1005 create cache dir: %w", err)
	}
	s := &Store{
		path:     filepath.Join(dir, "hosts.json"),
		endpoint: queryURL,
		client:   &http.Client{Timeout: 10 * time.Second},
		entries:  map[string]Entry{},
	}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, &s.entries)
	}
	return s, nil
}

// Get 返回主机的缓存结果, 不触发查询。
func (s *Store) Get(host string) Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[normalize(host)]
}

// Refresh 查询缺失或过期的主机, 有结果变化时返回 true。已有一轮在跑时直接返回 false。
// enabled 在每次查询前调用, 返回 false 时本轮停下 (用户中途关闭了查询)。
func (s *Store) Refresh(ctx context.Context, hosts []string, enabled func() bool) bool {
	now := time.Now()
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	var todo []string
	seen := map[string]bool{}
	for _, h := range hosts {
		h = normalize(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		if e, ok := s.entries[h]; !ok || e.stale(now) {
			todo = append(todo, h)
		}
	}
	if len(todo) == 0 {
		s.mu.Unlock()
		return false
	}
	s.running = true
	gen := s.gen
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	changed := false
	for _, h := range todo {
		if ctx.Err() != nil || !enabled() {
			break
		}
		e, retryLater := s.lookup(ctx, h)
		if retryLater {
			// 被限流: 本轮停下, 不写负缓存, 下次刷新列表时再查。
			break
		}
		e.CheckedAt = time.Now()
		s.mu.Lock()
		if s.gen != gen {
			s.mu.Unlock()
			return false
		}
		old, had := s.entries[h]
		s.entries[h] = e
		s.mu.Unlock()
		if !had || old.CountryCode != e.CountryCode || old.Country != e.Country {
			changed = true
		}
	}
	return s.saveIf(gen) && changed
}

// lookup 解析主机并查国家。retryLater 为 true 表示被限流, 结果不应缓存。
func (s *Store) lookup(ctx context.Context, host string) (e Entry, retryLater bool) {
	ip, err := resolve(ctx, host)
	if err != nil {
		return Entry{Failed: true}, false
	}
	e.IP = ip.String()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || isCGNAT(ip) {
		// 私网 / 保留地址没有国家, 不浪费一次请求。
		return e, false
	}
	code, name, status, err := s.query(ctx, e.IP)
	if status == http.StatusTooManyRequests {
		return e, true
	}
	if err != nil {
		e.Failed = true
		return e, false
	}
	e.CountryCode, e.Country = code, name
	return e, false
}

func resolve(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no address for %s", host)
	}
	// 优先 IPv4: 与大多数 SSH 客户端的默认选择一致。
	for _, a := range addrs {
		if a.IP.To4() != nil {
			return a.IP, nil
		}
	}
	return addrs[0].IP, nil
}

// query 调 ip.czl.net, 按响应体的 code 判定成败; 返回 HTTP 状态码以便识别限流。
func (s *Store) query(ctx context.Context, ip string) (code, name string, status int, err error) {
	body, _ := json.Marshal(map[string]string{"ip": ip})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	var r struct {
		Code int `json:"code"`
		Data *struct {
			Country     string `json:"country"`
			CountryCode string `json:"countryCode"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", resp.StatusCode, err
	}
	if r.Code == http.StatusTooManyRequests {
		return "", "", http.StatusTooManyRequests, fmt.Errorf("rate limited")
	}
	if r.Code != http.StatusOK || r.Data == nil {
		return "", "", resp.StatusCode, fmt.Errorf("query %s: %d %s", ip, r.Code, r.Msg)
	}
	cc := strings.ToUpper(strings.TrimSpace(r.Data.CountryCode))
	if !countryCodeRe.MatchString(cc) {
		cc = ""
	}
	return cc, r.Data.Country, resp.StatusCode, nil
}

// saveIf 在缓存代数仍为 gen 时写盘, 返回是否写了; 在锁内进行, 与 Clear 删除文件互斥。
func (s *Store) saveIf(gen int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return false
	}
	s.writeLocked()
	return true
}

func (s *Store) save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeLocked()
}

func (s *Store) writeLocked() {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, append(data, '\n'), 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// Prune 删掉不再出现在列表里的主机, 让缓存不随增删连接无限增长。
func (s *Store) Prune(hosts []string) {
	keep := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		keep[normalize(h)] = true
	}
	s.mu.Lock()
	removed := false
	for h := range s.entries {
		if !keep[h] {
			delete(s.entries, h)
			removed = true
		}
	}
	s.mu.Unlock()
	if removed {
		s.save()
	}
}

// Clear 清空缓存并删除缓存文件。
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = map[string]Entry{}
	s.gen++
	_ = os.Remove(s.path)
}

func normalize(host string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isCGNAT(ip net.IP) bool { return cgnat.Contains(ip) }

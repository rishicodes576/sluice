// Package budget enforces per-key spend and token caps over rolling windows,
// letting operators bound the cost of each API key.
package budget

import (
	"sync"
	"time"
)

// Window is the reset cadence for a budget.
type Window string

const (
	// Daily resets every 24h.
	Daily Window = "daily"
	// Monthly resets every ~30d.
	Monthly Window = "monthly"
)

func (w Window) duration() time.Duration {
	if w == Monthly {
		return 30 * 24 * time.Hour
	}
	return 24 * time.Hour
}

type account struct {
	cost      float64
	tokens    int64
	windowEnd time.Time
}

// Manager tracks spend per key and enforces optional caps.
type Manager struct {
	maxCost   float64 // USD per window; 0 disables
	maxTokens int64   // tokens per window; 0 disables
	window    time.Duration
	now       func() time.Time

	mu       sync.Mutex
	accounts map[string]*account
}

// New creates a budget manager. Zero caps mean "unlimited".
func New(maxCostUSD float64, maxTokens int64, w Window) *Manager {
	return &Manager{
		maxCost: maxCostUSD, maxTokens: maxTokens, window: w.duration(),
		now: time.Now, accounts: make(map[string]*account),
	}
}

func (m *Manager) acct(key string, now time.Time) *account {
	a, ok := m.accounts[key]
	if !ok || now.After(a.windowEnd) {
		a = &account{windowEnd: now.Add(m.window)}
		m.accounts[key] = a
	}
	return a
}

// Allow reports whether key is still within budget (checked before a request).
func (m *Manager) Allow(key string) bool {
	if m.maxCost <= 0 && m.maxTokens <= 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.acct(key, m.now())
	if m.maxCost > 0 && a.cost >= m.maxCost {
		return false
	}
	if m.maxTokens > 0 && a.tokens >= m.maxTokens {
		return false
	}
	return true
}

// Record adds spend and tokens against key after a request completes.
func (m *Manager) Record(key string, costUSD float64, tokens int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.acct(key, m.now())
	a.cost += costUSD
	a.tokens += tokens
}

// Usage is a snapshot of a key's consumption in the current window.
type Usage struct {
	CostUSD    float64 `json:"cost_usd"`
	Tokens     int64   `json:"tokens"`
	MaxCostUSD float64 `json:"max_cost_usd"`
	MaxTokens  int64   `json:"max_tokens"`
}

// Usage returns the current-window usage for key.
func (m *Manager) Usage(key string) Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.acct(key, m.now())
	return Usage{CostUSD: a.cost, Tokens: a.tokens, MaxCostUSD: m.maxCost, MaxTokens: m.maxTokens}
}

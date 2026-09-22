package budget

import (
	"testing"
	"time"
)

func TestBudgetCostCap(t *testing.T) {
	m := New(1.0, 0, Daily)
	if !m.Allow("k") {
		t.Fatal("should allow at zero spend")
	}
	m.Record("k", 0.6, 100)
	if !m.Allow("k") {
		t.Fatal("still under cap")
	}
	m.Record("k", 0.6, 100)
	if m.Allow("k") {
		t.Fatal("should block once cost cap exceeded")
	}
	u := m.Usage("k")
	if u.CostUSD < 1.2 || u.Tokens != 200 {
		t.Fatalf("usage = %+v", u)
	}
}

func TestBudgetWindowReset(t *testing.T) {
	base := time.Now()
	m := New(1.0, 0, Daily)
	m.now = func() time.Time { return base }
	m.Record("k", 2.0, 0)
	if m.Allow("k") {
		t.Fatal("over cap before reset")
	}
	m.now = func() time.Time { return base.Add(25 * time.Hour) }
	if !m.Allow("k") {
		t.Fatal("new window should reset the budget")
	}
}

func TestBudgetUnlimited(t *testing.T) {
	m := New(0, 0, Monthly)
	m.Record("k", 1000, 1e9)
	if !m.Allow("k") {
		t.Fatal("zero caps mean unlimited")
	}
}

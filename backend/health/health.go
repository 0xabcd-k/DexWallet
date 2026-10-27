package health

import (
	"context"
	"sync"
	"time"
)

type Check struct {
	Name     string
	Required bool
	Run      func(context.Context) error
}
type State struct {
	Name      string
	Required  bool
	CheckedAt time.Time
	Err       error
}
type Monitor struct {
	mu        sync.RWMutex
	states    []State
	stopping  bool
	freshness time.Duration
	now       func() time.Time
}

func New(checks []Check, freshness time.Duration) *Monitor {
	m := &Monitor{freshness: freshness, now: time.Now}
	for _, c := range checks {
		m.states = append(m.states, State{Name: c.Name, Required: c.Required})
	}
	return m
}
func (m *Monitor) Ready() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.stopping {
		return false
	}
	for _, s := range m.states {
		if s.Required && (s.CheckedAt.IsZero() || s.Err != nil || m.now().Sub(s.CheckedAt) > m.freshness) {
			return false
		}
	}
	return true
}
func (m *Monitor) Stop()          { m.mu.Lock(); m.stopping = true; m.mu.Unlock() }
func (m *Monitor) Stopping() bool { m.mu.RLock(); defer m.mu.RUnlock(); return m.stopping }
func (m *Monitor) Snapshot() []State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]State(nil), m.states...)
}
func (m *Monitor) Refresh(ctx context.Context, checks []Check, timeout time.Duration, report func(State)) {
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func(i int, c Check) {
			defer wg.Done()
			call, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			err := c.Run(call)
			if err == nil {
				err = call.Err()
			}
			state := State{Name: c.Name, Required: c.Required, CheckedAt: m.now(), Err: err}
			m.mu.Lock()
			old := m.states[i]
			m.states[i] = state
			m.mu.Unlock()
			if report != nil && (old.CheckedAt.IsZero() || (old.Err == nil) != (err == nil)) {
				report(state)
			}
		}(i, c)
	}
	wg.Wait()
}
func (m *Monitor) Run(ctx context.Context, checks []Check, interval, timeout time.Duration, report func(State)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Refresh(ctx, checks, timeout, report)
		}
	}
}

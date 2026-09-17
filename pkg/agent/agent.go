package agent

import (
	"sync"
	"time"
)

// AgentState represents lifecycle state of a coordinated subagent.
type AgentState string

const (
	StateIdle      AgentState = "idle"
	StateRunning   AgentState = "running"
	StateWaiting   AgentState = "waiting"
	StateCompleted AgentState = "completed"
	StateFailed    AgentState = "failed"
)

// AgentInfo describes a registered agent or sub-agent instance.
type AgentInfo struct {
	ID        string     `json:"id"`
	Role      string     `json:"role"`
	State     AgentState `json:"state"`
	StartedAt time.Time  `json:"started_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Coordinator tracks and delegates tasks among multiple agents.
type Coordinator struct {
	mu     sync.RWMutex
	agents map[string]*AgentInfo
}

// NewCoordinator creates a new multi-agent coordinator instance.
func NewCoordinator() *Coordinator {
	return &Coordinator{
		agents: make(map[string]*AgentInfo),
	}
}

// Register adds or updates an agent in the registry.
func (c *Coordinator) Register(id, role string) *AgentInfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	info := &AgentInfo{
		ID:        id,
		Role:      role,
		State:     StateRunning,
		StartedAt: now,
		UpdatedAt: now,
	}
	c.agents[id] = info
	return info
}

// UpdateState transitions an agent's state.
func (c *Coordinator) UpdateState(id string, state AgentState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if info, ok := c.agents[id]; ok {
		info.State = state
		info.UpdatedAt = time.Now()
	}
}

// List returns all active agent snapshots.
func (c *Coordinator) List() []AgentInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]AgentInfo, 0, len(c.agents))
	for _, info := range c.agents {
		out = append(out, *info)
	}
	return out
}

package stepfunctions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aircwo-systems/tarn/internal/config"
	"github.com/aircwo-systems/tarn/pkg/types"
)

// Store is an in-memory store for state machines and executions with optional
// JSON snapshot persistence. It mirrors the persistence pattern used by the
// other Tarn services (in-memory maps + a dirty flag + a periodic flusher).
type Store struct {
	mu         sync.RWMutex
	dirty      atomic.Bool
	cfg        *config.Config
	machines   map[string]*types.StateMachine // key: state machine ARN
	executions map[string]*types.Execution    // key: execution ARN
}

// NewStore creates an empty store bound to cfg.
func NewStore(cfg *config.Config) *Store {
	return &Store{
		cfg:        cfg,
		machines:   make(map[string]*types.StateMachine),
		executions: make(map[string]*types.Execution),
	}
}

type snapshot struct {
	StateMachines []*types.StateMachine `json:"stateMachines"`
	Executions    []*types.Execution    `json:"executions"`
}

// Init starts the background flusher and restores any persisted snapshot. Any
// execution still marked RUNNING in the snapshot is rewritten to ABORTED, since
// executions are not resumed across restarts in this MVP.
func (s *Store) Init() error {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return nil
	}
	go s.startFlusher()

	if err := os.MkdirAll(s.cfg.StepFunctionsDir(), 0o755); err != nil {
		return fmt.Errorf("create stepfunctions dir: %w", err)
	}

	data, err := os.ReadFile(s.cfg.StepFunctionsStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read stepfunctions state: %w", err)
	}
	if len(data) == 0 {
		return nil
	}

	var snap snapshot
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&snap); err != nil {
		return fmt.Errorf("decode stepfunctions state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sm := range snap.StateMachines {
		if sm == nil || sm.Arn == "" {
			continue
		}
		s.machines[sm.Arn] = cloneStateMachine(sm)
	}
	for _, ex := range snap.Executions {
		if ex == nil || ex.Arn == "" {
			continue
		}
		if ex.Status == types.ExecutionStatusRunning {
			ex.Status = types.ExecutionStatusAborted
		}
		s.executions[ex.Arn] = cloneExecution(ex)
	}
	return nil
}

// SaveStateMachine inserts or replaces a state machine.
func (s *Store) SaveStateMachine(sm *types.StateMachine) error {
	if sm == nil || sm.Arn == "" {
		return fmt.Errorf("state machine ARN is required")
	}
	s.mu.Lock()
	s.machines[sm.Arn] = cloneStateMachine(sm)
	s.mu.Unlock()
	return s.persist()
}

// GetStateMachine returns the state machine with the given ARN.
func (s *Store) GetStateMachine(arn string) (*types.StateMachine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sm, ok := s.machines[arn]
	if !ok {
		return nil, fmt.Errorf("state machine %s not found", arn)
	}
	return cloneStateMachine(sm), nil
}

// DeleteStateMachine removes a state machine by ARN.
func (s *Store) DeleteStateMachine(arn string) error {
	s.mu.Lock()
	if _, ok := s.machines[arn]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("state machine %s not found", arn)
	}
	delete(s.machines, arn)
	s.mu.Unlock()
	return s.persist()
}

// ListStateMachines returns all state machines sorted by ARN.
func (s *Store) ListStateMachines() []*types.StateMachine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.StateMachine, 0, len(s.machines))
	for _, sm := range s.machines {
		out = append(out, cloneStateMachine(sm))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Arn < out[j].Arn })
	return out
}

// SaveExecution inserts or replaces an execution.
func (s *Store) SaveExecution(ex *types.Execution) error {
	if ex == nil || ex.Arn == "" {
		return fmt.Errorf("execution ARN is required")
	}
	s.mu.Lock()
	s.executions[ex.Arn] = cloneExecution(ex)
	s.mu.Unlock()
	return s.persist()
}

// GetExecution returns the execution with the given ARN.
func (s *Store) GetExecution(arn string) (*types.Execution, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ex, ok := s.executions[arn]
	if !ok {
		return nil, fmt.Errorf("execution %s not found", arn)
	}
	return cloneExecution(ex), nil
}

// ListExecutions returns all executions sorted by start date (newest first).
func (s *Store) ListExecutions() []*types.Execution {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*types.Execution, 0, len(s.executions))
	for _, ex := range s.executions {
		out = append(out, cloneExecution(ex))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartDate.After(out[j].StartDate) })
	return out
}

func (s *Store) persist() error {
	s.dirty.Store(true)
	return nil
}

func (s *Store) startFlusher() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		if s.dirty.Swap(false) {
			s.flushToDisk()
		}
	}
}

func (s *Store) flushToDisk() {
	if s.cfg == nil || !s.cfg.PersistenceEnabled {
		return
	}

	s.mu.RLock()
	snap := snapshot{
		StateMachines: make([]*types.StateMachine, 0, len(s.machines)),
		Executions:    make([]*types.Execution, 0, len(s.executions)),
	}
	for _, sm := range s.machines {
		snap.StateMachines = append(snap.StateMachines, cloneStateMachine(sm))
	}
	for _, ex := range s.executions {
		snap.Executions = append(snap.Executions, cloneExecution(ex))
	}
	s.mu.RUnlock()

	sort.Slice(snap.StateMachines, func(i, j int) bool { return snap.StateMachines[i].Arn < snap.StateMachines[j].Arn })
	sort.Slice(snap.Executions, func(i, j int) bool { return snap.Executions[i].Arn < snap.Executions[j].Arn })

	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.cfg.StepFunctionsDir(), 0o755); err != nil {
		return
	}

	tmp, err := os.CreateTemp(s.cfg.StepFunctionsDir(), "state-*.json.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, s.cfg.StepFunctionsStatePath()); err != nil {
		_ = os.Remove(tmpPath)
	}
}

func cloneStateMachine(src *types.StateMachine) *types.StateMachine {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Tags != nil {
		dst.Tags = make(map[string]string, len(src.Tags))
		for k, v := range src.Tags {
			dst.Tags[k] = v
		}
	}
	return &dst
}

func cloneExecution(src *types.Execution) *types.Execution {
	if src == nil {
		return nil
	}
	dst := *src
	if src.StopDate != nil {
		t := *src.StopDate
		dst.StopDate = &t
	}
	if len(src.History) > 0 {
		dst.History = make([]types.HistoryEvent, len(src.History))
		copy(dst.History, src.History)
	}
	return &dst
}

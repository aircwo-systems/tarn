package lambda

import (
	"context"
	"fmt"
	"time"

	"github.com/openstack-project/openstack/internal/config"
	"github.com/openstack-project/openstack/internal/engine"
	"github.com/openstack-project/openstack/pkg/types"
)

// Service implements Lambda business logic.
type Service struct {
	cfg    *config.Config
	store  *Store
	engine *engine.Engine
	pool   *engine.WarmPool
}

// NewService creates a new Lambda service.
func NewService(cfg *config.Config, store *Store, eng *engine.Engine, pool *engine.WarmPool) *Service {
	return &Service{
		cfg:    cfg,
		store:  store,
		engine: eng,
		pool:   pool,
	}
}

// CreateFunction creates a new Lambda function.
func (s *Service) CreateFunction(ctx context.Context, fn *types.FunctionConfig, code []byte) (*types.FunctionConfig, error) {
	if s.store.FunctionExists(fn.FunctionName) {
		return nil, fmt.Errorf("function %s already exists", fn.FunctionName)
	}

	// Set defaults
	if fn.Timeout == 0 {
		fn.Timeout = s.cfg.LambdaDefaultTimeout
	}
	if fn.MemorySize == 0 {
		fn.MemorySize = s.cfg.LambdaDefaultMemory
	}
	if fn.Version == "" {
		fn.Version = "$LATEST"
	}

	fn.FunctionArn = fmt.Sprintf("arn:aws:lambda:%s:%s:function:%s", s.cfg.Region, s.cfg.AccountID, fn.FunctionName)
	fn.State = types.FunctionStateActive
	fn.LastModified = time.Now()

	// Save code
	if len(code) > 0 {
		hash, err := s.store.SaveCode(fn.FunctionName, code)
		if err != nil {
			return nil, fmt.Errorf("failed to save code: %w", err)
		}
		fn.CodeSHA256 = hash
		fn.CodeSize = int64(len(code))
	}

	// Save config
	if err := s.store.SaveFunction(fn); err != nil {
		return nil, fmt.Errorf("failed to save function: %w", err)
	}

	// Pull runtime image in background
	go func() {
		pullCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		exists, _ := s.engine.ImageExists(pullCtx, fn.Runtime)
		if !exists {
			_ = s.engine.PullImage(pullCtx, fn.Runtime)
		}
	}()

	return fn, nil
}

// GetFunction retrieves a function by name.
func (s *Service) GetFunction(name string) (*types.FunctionConfig, error) {
	return s.store.GetFunction(name)
}

// ListFunctions returns all functions.
func (s *Service) ListFunctions() ([]*types.FunctionConfig, error) {
	return s.store.ListFunctions()
}

// DeleteFunction removes a function and its container.
func (s *Service) DeleteFunction(ctx context.Context, name string) error {
	// Stop warm container if running
	if info, ok := s.engine.GetContainer(name); ok {
		_ = s.engine.StopContainer(ctx, info.ID)
		_ = s.engine.RemoveContainer(ctx, info.ID)
	}

	return s.store.DeleteFunction(name)
}

// UpdateFunctionCode replaces function code.
func (s *Service) UpdateFunctionCode(ctx context.Context, name string, code []byte) (*types.FunctionConfig, error) {
	fn, err := s.store.GetFunction(name)
	if err != nil {
		return nil, err
	}

	hash, err := s.store.SaveCode(name, code)
	if err != nil {
		return nil, fmt.Errorf("failed to save code: %w", err)
	}

	fn.CodeSHA256 = hash
	fn.CodeSize = int64(len(code))
	fn.LastModified = time.Now()

	// Evict warm container so next invoke uses new code
	if info, ok := s.engine.GetContainer(name); ok {
		_ = s.engine.StopContainer(ctx, info.ID)
		_ = s.engine.RemoveContainer(ctx, info.ID)
	}

	if err := s.store.SaveFunction(fn); err != nil {
		return nil, err
	}

	return fn, nil
}

// Invoke executes a Lambda function.
func (s *Service) Invoke(ctx context.Context, input *types.InvokeInput) (*types.InvokeOutput, error) {
	fn, err := s.store.GetFunction(input.FunctionName)
	if err != nil {
		return nil, err
	}

	if fn.State != types.FunctionStateActive {
		return nil, fmt.Errorf("function %s is not active (state: %s)", fn.FunctionName, fn.State)
	}

	// DryRun just validates
	if input.InvocationType == "DryRun" {
		return &types.InvokeOutput{StatusCode: 204}, nil
	}

	codeDir := s.store.GetCodeDir(fn.FunctionName)

	// Check for warm container
	info, warm := s.engine.GetContainer(fn.FunctionName)
	if !warm {
		// Create and start a new container
		info, err = s.engine.CreateContainer(ctx, fn, codeDir, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create container: %w", err)
		}
		if err := s.engine.StartContainer(ctx, info.ID); err != nil {
			return nil, fmt.Errorf("failed to start container: %w", err)
		}
	}

	s.pool.Touch(fn.FunctionName)

	// TODO: Phase 3 — Send event payload to container via RIE and collect response.
	// For now, return a placeholder indicating the container was started.
	return &types.InvokeOutput{
		StatusCode:      200,
		Payload:         []byte(`{"message": "invoke not yet wired to RIE — container started successfully"}`),
		ExecutedVersion: fn.Version,
	}, nil
}

package goose

import (
	"context"
	"fmt"
	"log"

	"github.com/innomon/agentic/pkg/config"
	"github.com/innomon/agentic/pkg/registry"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
)

// MultiRunner manages multiple in-process ADK runners.
type MultiRunner struct {
	runners map[string]*runner.Runner // agentName -> runner
	sessionService session.Service
}

// NewMultiRunner initializes runners for all agents defined in the agentic config.
func NewMultiRunner(ctx context.Context, configPath string) (*MultiRunner, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load agentic config: %w", err)
	}

	reg := registry.New(cfg)
	lc, err := reg.BuildLauncherConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("build launcher config: %w", err)
	}

	mr := &MultiRunner{
		runners:        make(map[string]*runner.Runner),
		sessionService: lc.SessionService,
	}

	for agentName := range cfg.Agents {
		ag, err := registry.Get[agent.Agent](ctx, reg, agentName)
		if err != nil {
			log.Printf("Warning: failed to load agent %s: %v", agentName, err)
			continue
		}

		r, err := runner.New(runner.Config{
			AppName:        agentName,
			Agent:          ag,
			SessionService: mr.sessionService,
		})
		if err != nil {
			log.Printf("Warning: failed to create runner for agent %s: %v", agentName, err)
			continue
		}
		mr.runners[agentName] = r
		log.Printf("Registered local agent: %s", agentName)
	}

	return mr, nil
}

// GetRunner returns the runner for the given agent name.
func (mr *MultiRunner) GetRunner(agentName string) (*runner.Runner, bool) {
	r, ok := mr.runners[agentName]
	return r, ok
}

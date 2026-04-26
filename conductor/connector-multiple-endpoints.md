# Objective
Create a `multi-connector` to support multiple AppIds , this will extend the capabilities of cmd/connector/main.go
(do not modify this file, add a new one), to run multiple adk instances in-process. 

Its config file will have an array of AppIds and agentic config files, see: https://github.com/innomon/agentic (locally available at ../agentic , do not modify it, treat as readonly)

it will read the config file, create a key/val with key as appid and for value will be instance of `runner.Runner`
see 

loads an agentic `config.yaml` file, instantiates the defined agents in-process, and routes requests to them using `runner.Runner`

**Local Initialization**:
   - On startup, if the endpoint is `"local"`, parse the agentic config using `github.com/innomon/agentic/pkg/config`.
   - Instantiate a `registry.Registry` and build a `launcher.Config` to obtain a `session.Service`.
   - Iterate over all agents defined in the config. For each agent, use `registry.Get[agent.Agent]` to load it and wrap it in a `runner.Runner` (`google.golang.org/adk/runner`).
   - Store these runners in an in-memory map keyed by the agent name.

Sample code :

```go
import(
   //...

	"github.com/innomon/agentic/pkg/config"
	"github.com/innomon/agentic/pkg/registry"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
	"gopkg.in/yaml.v3"
)

//...

func (s *Server) initLocalADK() error {
	ctx := context.Background()
	cfgPath := s.config.Proxy.ADK.ConfigPath
	if cfgPath == "" {
		return fmt.Errorf("config_path is required for local endpoint")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load local adk config: %w", err)
	}

	s.localRegistry = registry.New(cfg)

	lc, err := s.localRegistry.BuildLauncherConfig(ctx)
	if err != nil {
		return fmt.Errorf("build launcher config: %w", err)
	}
	s.sessionService = lc.SessionService

	for agentName := range cfg.Agents {
		ag, err := registry.Get[agent.Agent](ctx, s.localRegistry, agentName)
		if err != nil {
			log.Printf("Warning: failed to load agent %s: %v", agentName, err)
			continue
		}

		r, err := runner.New(runner.Config{
			AppName:        agentName,
			Agent:          ag,
			SessionService: s.sessionService,
		})
		if err != nil {
			log.Printf("Warning: failed to create runner for agent %s: %v", agentName, err)
			continue
		}
		s.localRunners[agentName] = r
		log.Printf("Registered local agent: %s", agentName)
	}

	return nil
}

func (s *Server) createSession(ctx context.Context, userID string) (string, error) {
	url := fmt.Sprintf("%s/api/apps/%s/users/%s/sessions",
		s.config.Proxy.ADK.Endpoint,
		s.config.Proxy.ADK.AppName,
		userID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("create session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session failed: %s", string(body))
	}

	var session ADKSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return "", fmt.Errorf("decode session response: %w", err)
	}

	return session.ID, nil
}


```



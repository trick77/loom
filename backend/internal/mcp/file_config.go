package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// fileConfig is the on-disk MCP server file in the standard `mcpServers` format
// that vendors hand out (the same shape Claude Desktop / mcp.json uses). It is
// deliberately decoupled from ServerConfig so the file can speak `type` (http,
// stdio) while Loom's internal model speaks `transport` (streamable-http, stdio).
type fileConfig struct {
	MCPServers map[string]fileServer `json:"mcpServers"`
}

// fileServer mirrors one entry under `mcpServers`. Every string value supports
// ${VAR} interpolation so secrets stay in the environment and never live in the
// committed file.
type fileServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Tools   []string          `json:"tools"`
}

// envRefPattern matches ${NAME} references. Only the braced form is supported so
// interpolation is unambiguous (a bare $VAR in a header or URL stays literal).
var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// LoadServersFromFile reads MCP servers from a JSON file in the standard
// `mcpServers` format and returns them as Loom ServerConfigs keyed by server
// name. An empty path or a missing file is a no-op (returns nil, nil) so the
// default path can be safe when no file is present; a present-but-invalid file
// is a hard error so misconfiguration surfaces at startup rather than silently
// dropping a server. lookupEnv resolves ${VAR} references (pass os.LookupEnv in
// production; a fake map in tests).
func LoadServersFromFile(path string, lookupEnv func(string) (string, bool)) (map[string]ServerConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read MCP servers file %q: %w", path, err)
	}
	var parsed fileConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse MCP servers file %q: %w", path, err)
	}
	out := make(map[string]ServerConfig, len(parsed.MCPServers))
	// Resolve in name order so any error is deterministic across runs.
	names := make([]string, 0, len(parsed.MCPServers))
	for name := range parsed.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("MCP servers file %q: server name must not be blank", path)
		}
		cfg, err := parsed.MCPServers[name].toServerConfig(lookupEnv)
		if err != nil {
			return nil, fmt.Errorf("MCP servers file %q: server %q: %w", path, name, err)
		}
		out[name] = cfg
	}
	return out, nil
}

// toServerConfig maps the file shape to a ServerConfig: resolves the transport
// from `type`, interpolates ${VAR} across every string value, and validates the
// minimum fields the chosen transport needs.
func (f fileServer) toServerConfig(lookupEnv func(string) (string, bool)) (ServerConfig, error) {
	transport, err := transportForType(f.Type)
	if err != nil {
		return ServerConfig{}, err
	}
	url, err := expandEnvRefs(f.URL, lookupEnv)
	if err != nil {
		return ServerConfig{}, err
	}
	headers, err := expandEnvMap(f.Headers, lookupEnv)
	if err != nil {
		return ServerConfig{}, err
	}
	command, err := expandEnvRefs(f.Command, lookupEnv)
	if err != nil {
		return ServerConfig{}, err
	}
	args, err := expandEnvSlice(f.Args, lookupEnv)
	if err != nil {
		return ServerConfig{}, err
	}
	envVars, err := expandEnvMap(f.Env, lookupEnv)
	if err != nil {
		return ServerConfig{}, err
	}
	cfg := ServerConfig{
		Transport: transport,
		URL:       url,
		Headers:   headers,
		Command:   command,
		Args:      args,
		Env:       envVars,
		Tools:     f.Tools,
	}
	switch transport {
	case TransportStreamableHTTP:
		if strings.TrimSpace(cfg.URL) == "" {
			return ServerConfig{}, fmt.Errorf("transport %q requires a url", transport)
		}
	case TransportStdio:
		if strings.TrimSpace(cfg.Command) == "" {
			return ServerConfig{}, fmt.Errorf("transport %q requires a command", transport)
		}
	}
	return cfg, nil
}

// transportForType maps the file's `type` to a Loom transport. "http" and the
// canonical "streamable-http" both select the Streamable HTTP transport; an
// empty type defaults to it. Anything else (e.g. "sse", which Loom's client does
// not speak as a transport) is rejected so the failure is explicit.
func transportForType(t string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(t)) {
	case "", "http", "streamable-http", "streamablehttp":
		return TransportStreamableHTTP, nil
	case "stdio":
		return TransportStdio, nil
	default:
		return "", fmt.Errorf("unsupported type %q (use \"http\" or \"stdio\")", t)
	}
}

// expandEnvRefs replaces every ${VAR} in s with its environment value, returning
// an error that names any referenced variable that is unset — an empty secret is
// never silently substituted.
func expandEnvRefs(s string, lookupEnv func(string) (string, bool)) (string, error) {
	var missing []string
	out := envRefPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := envRefPattern.FindStringSubmatch(match)[1]
		value, ok := lookupEnv(name)
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return value
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unset environment variable(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}

func expandEnvMap(in map[string]string, lookupEnv func(string) (string, bool)) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		expanded, err := expandEnvRefs(value, lookupEnv)
		if err != nil {
			return nil, err
		}
		out[key] = expanded
	}
	return out, nil
}

func expandEnvSlice(in []string, lookupEnv func(string) (string, bool)) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]string, len(in))
	for i, value := range in {
		expanded, err := expandEnvRefs(value, lookupEnv)
		if err != nil {
			return nil, err
		}
		out[i] = expanded
	}
	return out, nil
}

// Package oic hosts applications without interpreting their business actions.
package oic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Version int `yaml:"version" json:"version"`
	Host    struct {
		ID   string `yaml:"id" json:"id"`
		Name string `yaml:"name" json:"name"`
	} `yaml:"host" json:"host"`
	Channel struct {
		ID string `yaml:"id" json:"id"`
	} `yaml:"channel" json:"channel"`
	OIS struct {
		URL           string `yaml:"url" json:"url"`
		CredentialEnv string `yaml:"credential_env" json:"credential_env,omitempty"`
		CAFile        string `yaml:"ca_file" json:"ca_file,omitempty"`
	} `yaml:"ois" json:"ois"`
	StateDir  string                    `yaml:"state_dir" json:"state_dir"`
	AppConfig map[string]map[string]any `yaml:"app_config" json:"app_config,omitempty"`
}

type AppDefinition struct {
	Version int `yaml:"version" json:"version"`
	App     struct {
		Code string `yaml:"code" json:"code"`
	} `yaml:"app" json:"app"`
	Runtime map[string]any              `yaml:"runtime" json:"runtime,omitempty"`
	Actions map[string]ActionDefinition `yaml:"actions" json:"actions"`
}

type ActionDefinition struct {
	Description  string            `yaml:"description" json:"description"`
	InputSchema  map[string]any    `yaml:"input_schema" json:"input_schema"`
	InputMapping map[string]string `yaml:"input_mapping" json:"input_mapping,omitempty"`
	Handler      string            `yaml:"handler" json:"handler"`
	ResultFormat string            `yaml:"result_format,omitempty" json:"result_format,omitempty"`
	Runtime      map[string]any    `yaml:"runtime" json:"runtime,omitempty"`
}

type runtimeSpec struct {
	command []string
	key     string
	formats map[string]bool
}
type appSpec struct {
	definition      AppDefinition
	path, dir, hash string
	runtimes        map[string]runtimeSpec
	actionRuntime   map[string]string
}

func readConfig(path string) (Config, string, error) {
	var c Config
	abs, err := filepath.Abs(path)
	if err != nil {
		return c, "", err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return c, "", err
	}
	if err = yaml.Unmarshal(b, &c); err != nil {
		return c, "", err
	}
	if c.Version != 1 || c.Host.ID == "" || c.Channel.ID == "" || c.OIS.URL == "" {
		return c, "", fmt.Errorf("version 1, host.id, channel.id and ois.url are required")
	}
	if _, _, err := connectionEndpoint(c.OIS.URL, c.OIS.CAFile); err != nil {
		return c, "", err
	}
	base := filepath.Dir(abs)
	if c.StateDir == "" {
		c.StateDir = ".oic"
	}
	c.StateDir = resolvePath(base, c.StateDir)
	if c.OIS.CAFile != "" {
		c.OIS.CAFile = resolvePath(base, c.OIS.CAFile)
	}
	return c, abs, nil
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func readApp(path string, localConfig map[string]map[string]any) (*appSpec, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var d AppDefinition
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("app YAML: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("App YAML must contain exactly one document")
	}
	if d.Version != 1 || strings.TrimSpace(d.App.Code) == "" || len(d.Actions) == 0 {
		return nil, fmt.Errorf("version 1, app.code and at least one Action are required")
	}
	s := &appSpec{definition: d, path: abs, dir: filepath.Dir(abs), runtimes: map[string]runtimeSpec{}, actionRuntime: map[string]string{}}
	for name, a := range d.Actions {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(a.Description) == "" {
			return nil, fmt.Errorf("each Action requires a name and description")
		}
		if err := validateSchema(a.InputSchema, true); err != nil {
			return nil, fmt.Errorf("action %s: %w", name, err)
		}
		if a.ResultFormat != "" && a.ResultFormat != protocol.FormatJSON && a.ResultFormat != protocol.FormatContent {
			return nil, fmt.Errorf("action %s: unsupported result_format %q", name, a.ResultFormat)
		}
		if a.Handler == "" {
			a.Handler = name
		}
		for dest, src := range a.InputMapping {
			if dest == "" || src == "" {
				return nil, fmt.Errorf("action %s: input_mapping names must not be empty", name)
			}
		}
		d.Actions[name] = a
		effective := map[string]any{}
		for k, v := range d.Runtime {
			effective[k] = v
		}
		for k, v := range a.Runtime {
			effective[k] = v
		}
		key := "root"
		if len(a.Runtime) != 0 {
			key = "action:" + name
		}
		command, err := commandArray(effective["command"])
		if err != nil {
			return nil, fmt.Errorf("action %s runtime: %w", name, err)
		}
		rt := s.runtimes[key]
		if rt.formats == nil {
			rt = runtimeSpec{command: command, key: key, formats: map[string]bool{}}
		}
		format := a.ResultFormat
		if format == "" {
			format = protocol.FormatJSON
		}
		rt.formats[format] = true
		s.runtimes[key] = rt
		s.actionRuntime[name] = key
	}
	s.definition = d
	// Identity is based on parsed definitions, location and local configuration,
	// so whitespace-only YAML edits do not restart a live application.
	canonical, err := json.Marshal(struct {
		Definition AppDefinition
		Directory  string
		Config     map[string]any
	}{d, s.dir, localConfig[d.App.Code]})
	if err != nil {
		return nil, fmt.Errorf("app contains non-JSON data: %w", err)
	}
	sum := sha256.Sum256(canonical)
	s.hash = hex.EncodeToString(sum[:])
	return s, nil
}

func commandArray(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("command must be a non-empty string array")
	}
	command := make([]string, len(items))
	for i, v := range items {
		str, ok := v.(string)
		if !ok || (i == 0 && str == "") {
			return nil, fmt.Errorf("command entries must be strings and program must not be empty")
		}
		command[i] = str
	}
	return command, nil
}

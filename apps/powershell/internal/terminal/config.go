//go:build windows

// Package terminal implements PowerShell command execution and interactive
// sessions for the standalone Windows App.
package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Limits struct {
	MaxConcurrentSessions int `json:"max_concurrent_sessions"`
	SessionBufferBytes    int `json:"session_buffer_bytes"`
	MaxCmdBytes           int `json:"max_cmd_bytes"`
	MaxInputBytes         int `json:"max_input_bytes"`
	MaxOutputTokens       int `json:"max_output_tokens"`
	MaxRuntimeMS          int `json:"max_runtime_ms"`
	IdleTimeoutMS         int `json:"idle_timeout_ms"`
	FinishedSessionTTLMS  int `json:"finished_session_ttl_ms"`
	MaxFinishedSessions   int `json:"max_finished_sessions"`
}
type Dimensions struct {
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}
type Config struct {
	ShellKind  string            `json:"shell_kind"`
	ShellPath  string            `json:"shell_path"`
	DefaultCwd string            `json:"default_cwd"`
	Env        map[string]string `json:"env"`
	Limits     Limits            `json:"limits"`
	Terminal   Dimensions        `json:"terminal"`
}

func DefaultConfig() Config {
	return Config{Limits: Limits{8, 1 << 20, 65536, 65536, 20000, 1800000, 600000, 600000, 32}, Terminal: Dimensions{120, 40}}
}
func ParseConfig(data map[string]any) (Config, error) {
	c := DefaultConfig()
	b, e := json.Marshal(data)
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	return c, c.Validate()
}
func (c *Config) Validate() error {
	if c.ShellKind == "" {
		c.ShellKind = "powershell"
	}
	if c.ShellKind != "powershell" {
		return errors.New("PowerShell App requires shell_kind powershell")
	}
	if c.ShellPath == "" {
		c.ShellPath = "pwsh"
	}
	resolved, e := exec.LookPath(c.ShellPath)
	if e != nil {
		return fmt.Errorf("shell_path: %w", e)
	}
	c.ShellPath, e = filepath.Abs(resolved)
	if e != nil {
		return e
	}
	if c.DefaultCwd == "" {
		c.DefaultCwd, e = os.Getwd()
		if e != nil {
			return e
		}
	}
	c.DefaultCwd, e = filepath.Abs(c.DefaultCwd)
	if e != nil {
		return e
	}
	st, e := os.Stat(c.DefaultCwd)
	if e != nil || !st.IsDir() {
		return errors.New("default_cwd must be an existing directory")
	}
	b, _ := json.Marshal(c.Limits)
	var vals map[string]int
	_ = json.Unmarshal(b, &vals)
	for k, v := range vals {
		if v <= 0 {
			return fmt.Errorf("limits.%s must be positive", k)
		}
	}
	if c.Limits.SessionBufferBytes < 4 || c.Limits.SessionBufferBytes > 64<<20 {
		return errors.New("session_buffer_bytes must be between 4 and 67108864")
	}
	if c.Limits.MaxOutputTokens > 40000 {
		return errors.New("max_output_tokens exceeds the 1 MiB protocol bound")
	}
	if c.Terminal.Columns < 1 || c.Terminal.Columns > 32767 || c.Terminal.Rows < 1 || c.Terminal.Rows > 32767 {
		return errors.New("invalid terminal dimensions")
	}
	for k, v := range c.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return errors.New("invalid environment entry")
		}
	}
	return nil
}

// The App receives a controlled environment rather than copying host secrets.
func (c Config) environment() []string {
	keys := []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "TMPDIR", "SystemRoot", "WINDIR", "TEMP", "TMP", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "ProgramFiles", "ProgramFiles(x86)", "PSModulePath", "PATHEXT", "ComSpec"}
	env := map[string]string{}
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	env["TERM"] = "xterm-256color"
	for k, v := range c.Env {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
func (c Config) command(script string, tty bool) *exec.Cmd {
	args := []string{"-NoLogo", "-NoProfile"}
	if !tty {
		args = append(args, "-NonInteractive")
	}
	args = append(args, "-Command", script)
	cmd := exec.Command(c.ShellPath, args...)
	cmd.Env = c.environment()
	return cmd
}

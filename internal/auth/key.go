// Package auth resolves the OpenRouter API key from the places a user is
// likely to have already stored it: an explicit flag, the environment, a .env
// file in or above the working directory, or a credential file on disk. When
// nothing is found the caller prompts for the key and can persist it.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvVar is the environment variable consulted first after an explicit flag.
const EnvVar = "OPENROUTER_API_KEY"

// ErrNotFound reports that no key exists in any searched location.
var ErrNotFound = errors.New("no OpenRouter API key found")

// Key is a resolved API key together with the location it came from, so the
// UI can tell the user which source won.
type Key struct {
	Value  string
	Source string
}

// SearchPath returns the ordered locations Resolve inspects, for display when
// resolution fails. Each location appears once, in the order it is tried.
func SearchPath() []string {
	var paths []string
	seen := make(map[string]bool)
	add := func(path string) {
		if seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}

	add(fmt.Sprintf("$%s environment variable", EnvVar))
	if cwd, err := os.Getwd(); err == nil {
		for _, p := range envCandidates(cwd) {
			add(p)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		home = strings.TrimRight(home, string(os.PathSeparator))
		add(filepath.Join(home, ".env"))
		for _, p := range credentialCandidates(home) {
			add(p)
		}
	}
	return paths
}

// Resolve returns the first key found, searching explicit, then the
// environment, then .env files from the working directory up to the home
// directory or filesystem root, then known credential files. It returns
// ErrNotFound when every location is empty.
func Resolve(explicit string) (Key, error) {
	if k := strings.TrimSpace(explicit); k != "" {
		return Key{Value: k, Source: "--api-key flag"}, nil
	}
	if k := strings.TrimSpace(os.Getenv(EnvVar)); k != "" {
		return Key{Value: k, Source: fmt.Sprintf("$%s", EnvVar)}, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if k, path, ok := firstFromEnvFiles(envCandidates(cwd)); ok {
			return Key{Value: k, Source: displayPath(path)}, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Key{}, ErrNotFound
	}
	home = strings.TrimRight(home, string(os.PathSeparator))
	if k, path, ok := firstFromEnvFiles([]string{filepath.Join(home, ".env")}); ok {
		return Key{Value: k, Source: displayPath(path)}, nil
	}
	if k, path, ok := firstFromCredentialFiles(credentialCandidates(home)); ok {
		return Key{Value: k, Source: displayPath(path)}, nil
	}
	return Key{}, ErrNotFound
}

// Mask renders a key for display without exposing its secret material.
func Mask(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(none)"
	}
	if len(key) <= 14 {
		return key[:min(3, len(key))] + "..."
	}
	return key[:12] + "..." + key[len(key)-3:]

}

// CredentialPath is the file Save writes a prompted key to.
func CredentialPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "orusage", "credentials"), nil
}

// Save persists a key to the user credential file with owner-only
// permissions and returns the path written.
func Save(key string) (string, error) {
	path, err := CredentialPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	contents := fmt.Sprintf("# OpenRouter API key used by orusage\n%s=%s\n", EnvVar, strings.TrimSpace(key))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// envCandidates lists .env files to try, nearest directory first, walking up
// from dir. The walk stops at the home directory so a stray /tmp/.env is never
// read. Paths are compared after resolving symlinks, because a temporary
// directory and its /private twin are the same location on macOS.
func envCandidates(dir string) []string {
	home, _ := os.UserHomeDir()
	home = canonical(strings.TrimRight(home, string(os.PathSeparator)))
	dir = canonical(dir)
	var out []string
	for {
		out = append(out, filepath.Join(dir, ".env"))
		if dir == home || dir == filepath.Dir(dir) {
			break
		}
		dir = filepath.Dir(dir)
	}
	return out
}

func credentialCandidates(home string) []string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(configHome, "orusage", "credentials"),
		filepath.Join(home, ".dsh", ".credentials.yaml"),
		filepath.Join(configHome, "openrouter", "credentials"),
		filepath.Join(home, ".openrouter", "credentials"),
	}
}

func firstFromEnvFiles(paths []string) (string, string, bool) {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if v := lookup(ParseEnv(string(data)), EnvVar); v != "" {
			return v, p, true
		}
	}
	return "", "", false
}

func firstFromCredentialFiles(paths []string) (string, string, bool) {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		text := string(data)
		if v := lookup(ParseEnv(text), EnvVar); v != "" {
			return v, p, true
		}
		if v := lookup(parseYAML(text), EnvVar); v != "" {
			return v, p, true
		}
		if v := parseJSON(text); v != "" {
			return v, p, true
		}
	}
	return "", "", false
}

func lookup(values map[string]string, name string) string {
	return strings.TrimSpace(values[name])
}

// ParseEnv reads KEY=value pairs, ignoring comments and blank lines. It
// tolerates an `export ` prefix, single or double quotes, and CRLF endings.
func ParseEnv(text string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = unquote(value)
	}
	return out
}

func unquote(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "\r"))
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	// A trailing inline comment is only stripped outside quotes, which the
	// branch above already handled.
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return value
}

// parseYAML extracts flat `KEY: value` entries, which covers the credential
// files this tool reads without depending on a YAML parser.
func parseYAML(text string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = unquote(strings.TrimSpace(value))
		if name == "" || value == "" {
			continue
		}
		out[name] = value
	}
	return out
}

// parseJSON accepts credential files shaped as {"api_key": "..."} or
// {"OPENROUTER_API_KEY": "..."}.
func parseJSON(text string) string {
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		return ""
	}
	for _, field := range []string{EnvVar, "api_key", "apiKey", "key"} {
		if v, ok := doc[field].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// displayPath shortens a path to a ~-rooted or working-directory-relative form
// for the UI, keeping the absolute path when neither applies.
func displayPath(path string) string {
	path = canonical(path)
	if home, err := os.UserHomeDir(); err == nil {
		if rel, ok := relativeTo(path, canonical(strings.TrimRight(home, string(os.PathSeparator)))); ok {
			if rel == "." {
				return "~"
			}
			return "~/" + rel
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if rel, ok := relativeTo(path, canonical(cwd)); ok && rel != "." {
			return rel
		}
	}
	return path
}

// canonical resolves symlinks, falling back to the input when the path cannot
// be resolved.
func canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// relativeTo reports path relative to base when path is inside base.
func relativeTo(path, base string) (string, bool) {
	if base == "" {
		return "", false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return rel, true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

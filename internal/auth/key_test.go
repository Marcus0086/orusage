package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnv(t *testing.T) {
	text := "# comment\nexport OPENROUTER_API_KEY=\"sk-or-v1-abc\"\nOTHER=plain\nQUOTED='sq'\nEMPTY=\nNOTAPAIR\nINLINE=value # trailing\nCRLF=x\r\n"
	got := ParseEnv(text)
	want := map[string]string{
		"OPENROUTER_API_KEY": "sk-or-v1-abc",
		"OTHER":              "plain",
		"QUOTED":             "sq",
		"EMPTY":              "",
		"INLINE":             "value",
		"CRLF":               "x",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ParseEnv[%q] = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["NOTAPAIR"]; ok {
		t.Error("ParseEnv accepted a line without =")
	}
}

func TestParseYAMLAndJSON(t *testing.T) {
	yaml := "version: 1\nrefs:\n  BONSAI_API_KEY: bonsai\n  OPENROUTER_API_KEY: sk-or-v1-yamlvalue\n"
	if got := lookup(parseYAML(yaml), EnvVar); got != "sk-or-v1-yamlvalue" {
		t.Errorf("parseYAML = %q", got)
	}
	if got := parseJSON(`{"api_key":"sk-or-v1-jsonvalue"}`); got != "sk-or-v1-jsonvalue" {
		t.Errorf("parseJSON = %q", got)
	}
	if got := parseJSON(`{"nope":1}`); got != "" {
		t.Errorf("parseJSON accepted a document without a key field: %q", got)
	}
	if got := parseJSON(`not json`); got != "" {
		t.Errorf("parseJSON accepted invalid JSON: %q", got)
	}
}

func TestResolvePrefersExplicitThenEnvThenFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENROUTER_API_KEY=from-dotenv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "repo")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}

	if got, err := Resolve("explicit-key"); err != nil || got.Value != "explicit-key" || got.Source != "--api-key flag" {
		t.Fatalf("Resolve(explicit) = %+v, %v", got, err)
	}

	t.Setenv(EnvVar, "env-key")
	if got, err := Resolve(""); err != nil || got.Value != "env-key" {
		t.Fatalf("Resolve(env) = %+v, %v", got, err)
	}

	t.Setenv(EnvVar, "")
	got, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "from-dotenv" {
		t.Fatalf("Resolve(.env walk-up) = %+v, want the parent .env value", got)
	}
	if got.Source != "~/.env" {
		t.Errorf("source = %q, want ~/.env", got.Source)
	}
}

func TestResolveFindsDSHCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv(EnvVar, "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".dsh"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "version: 1\nrecords: {}\nrefs:\n  OPENROUTER_API_KEY: sk-or-v1-fromcredentials\n"
	if err := os.WriteFile(filepath.Join(dir, ".dsh", ".credentials.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "sk-or-v1-fromcredentials" || got.Source != "~/.dsh/.credentials.yaml" {
		t.Fatalf("Resolve(credentials) = %+v", got)
	}
}

func TestResolveNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))
	t.Setenv(EnvVar, "")
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(""); err == nil {
		t.Fatal("Resolve succeeded with no key anywhere")
	}
	if len(SearchPath()) == 0 {
		t.Error("SearchPath returned nothing to display")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	path, err := Save("sk-or-v1-saved")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
	t.Setenv(EnvVar, "")
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	_ = got
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"":                      "(none)",
		"short":                 "sho...",
		"sk-or-v1-a0e034a6e636": "sk-or-v1-a0e...636",
	}
	for in, want := range cases {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

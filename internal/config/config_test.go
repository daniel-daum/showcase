package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, s string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTokenFile(t *testing.T) {
	dir := t.TempDir()
	base := `{"gitea_url":"http://g:3000","user":"daniel","site_url":"https://s","repos":["a"]`

	tok := write(t, dir, "token", "abc123\n")
	c, err := Load(write(t, dir, "ok.json", base+`,"gitea_token_file":"`+tok+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.GiteaToken != "abc123" {
		t.Errorf("token = %q", c.GiteaToken)
	}
	if b, _ := json.Marshal(c); strings.Contains(string(b), "abc123") {
		t.Error("the token is marshalled with the config")
	}

	// the token never comes from config.json itself
	if _, err := Load(write(t, dir, "inline.json", base+`,"GiteaToken":"abc123"}`)); err == nil {
		t.Error("an inline token was accepted")
	}
	for name, body := range map[string]string{"empty": "\n", "two": "abc def\n"} {
		p := write(t, dir, name, body)
		if _, err := Load(write(t, dir, name+".json", base+`,"gitea_token_file":"`+p+`"}`)); err == nil {
			t.Errorf("%s token file was accepted", name)
		}
	}
	if _, err := Load(write(t, dir, "missing.json", base+`,"gitea_token_file":"/nonexistent"}`)); err == nil {
		t.Error("a missing token file was accepted")
	}

	c, err = Load(write(t, dir, "anon.json", base+`}`))
	if err != nil || c.GiteaToken != "" {
		t.Errorf("no token file: token %q, err %v", c.GiteaToken, err)
	}
}

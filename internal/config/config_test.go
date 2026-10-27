package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveIsPrivateAndLoadAppliesEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whq", "config.json")
	t.Setenv("WHQ_CONFIG", path)
	if err := Save(Config{Token: "whq_saved"}); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	t.Setenv("WHQ_API_URL", "http://app.webhookhq.test:3000")
	cfg, err := Load()
	if err != nil || cfg.Token != "whq_saved" || cfg.APIURL != "http://app.webhookhq.test:3000" {
		t.Fatalf("%+v %v", cfg, err)
	}
	t.Setenv("WHQ_TOKEN", "whq_env")
	if cfg, _ := Load(); cfg.Token != "whq_env" {
		t.Fatal(cfg.Token)
	}
}

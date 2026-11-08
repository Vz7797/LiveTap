package buildinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestVersionIsSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`).MatchString(Version) {
		t.Fatalf("VERSION %q 不是语义化版本号", Version)
	}
}

func TestEngineVersionMatchesVendoredBaseline(t *testing.T) {
	files, err := filepath.Glob("../../../deploy/patches/douyinlive-*-baseline.json")
	if err != nil || len(files) != 1 {
		t.Fatalf("expected exactly one engine baseline, got %v (%v)", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var baseline struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &baseline); err != nil || baseline.Version != EngineVersion {
		t.Fatalf("baseline version %q, EngineVersion %q (%v)", baseline.Version, EngineVersion, err)
	}
}

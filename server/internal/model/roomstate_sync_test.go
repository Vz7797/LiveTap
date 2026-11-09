package model

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The admin UI (web/src/lib/meta.ts) and the API docs (docs/api.md) restate the
// room states, their classes and the reason codes. This test fails as soon as
// one side changes without the others, naming what is missing.
func TestRoomStatesMatchWebAndDocs(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	meta := read(t, filepath.Join(root, "web", "src", "lib", "meta.ts"))
	api := read(t, filepath.Join(root, "docs", "api.md"))

	union := regexp.MustCompile(`(?s)export type RoomStatus =(.*?);`).FindStringSubmatch(meta)
	if union == nil {
		t.Fatal("web: RoomStatus type not found in meta.ts")
	}
	webStates := quoted(union[1])
	for _, s := range RoomStates {
		if !slices.Contains(webStates, string(s)) {
			t.Errorf("web: RoomStatus is missing %q", s)
		}
		if !strings.Contains(api, "| `"+string(s)+"` |") {
			t.Errorf("docs/api.md: state table is missing %q", s)
		}
	}
	if len(webStates) != len(RoomStates) {
		t.Errorf("web: RoomStatus has %v, server has %v", webStates, RoomStates)
	}

	classes := map[string]func(RoomState) bool{"isLive": RoomState.Live, "isAbnormal": RoomState.Abnormal, "isFinal": RoomState.Final}
	for name, class := range classes {
		line := regexp.MustCompile(`export const ` + name + ` = [^\n]*`).FindString(meta)
		if line == "" {
			t.Errorf("web: %s not found in meta.ts", name)
			continue
		}
		web := quoted(line)
		for _, s := range RoomStates {
			if class(s) != slices.Contains(web, string(s)) {
				t.Errorf("web: %s disagrees with the server about %q", name, s)
			}
		}
	}

	for _, code := range ReasonCodes {
		if !strings.Contains(api, "| `"+code+"` |") {
			t.Errorf("docs/api.md: reason code table is missing %q", code)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func quoted(s string) []string {
	out := []string{}
	for _, m := range regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

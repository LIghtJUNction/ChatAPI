package migrationplan

import (
	"testing"
	"testing/fstest"
)

func TestLatestVersion(t *testing.T) {
	steps := []Step{
		{Version: "0001_bootstrap", Name: "bootstrap"},
		{Version: "0002_more", Name: "more"},
	}
	if got := LatestVersion(steps); got != "0002_more" {
		t.Fatalf("unexpected latest version: %q", got)
	}
}

func TestValidate(t *testing.T) {
	valid := []Step{
		{Version: "0001_bootstrap", Name: "bootstrap", UpSQL: "CREATE TABLE bootstrap(id INTEGER);"},
		{Version: "0002_more", Name: "more", UpSQL: "CREATE INDEX idx_more ON bootstrap(id);"},
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("validate valid plan: %v", err)
	}

	for _, tc := range []struct {
		name  string
		steps []Step
	}{
		{
			name:  "empty",
			steps: nil,
		},
		{
			name: "empty version",
			steps: []Step{
				{Version: "", Name: "bootstrap", UpSQL: "SELECT 1;"},
			},
		},
		{
			name: "empty name",
			steps: []Step{
				{Version: "0001_bootstrap", Name: "", UpSQL: "SELECT 1;"},
			},
		},
		{
			name: "empty sql",
			steps: []Step{
				{Version: "0001_bootstrap", Name: "bootstrap"},
			},
		},
		{
			name: "unordered",
			steps: []Step{
				{Version: "0002_b", Name: "b", UpSQL: "SELECT 2;"},
				{Version: "0001_a", Name: "a", UpSQL: "SELECT 1;"},
			},
		},
	} {
		if err := Validate(tc.steps); err == nil {
			t.Fatalf("expected validation error for %s", tc.name)
		}
	}
}

func TestLoadSteps(t *testing.T) {
	steps, err := LoadSteps(fstest.MapFS{
		"sql/0002_more.up.sql":      {Data: []byte("SELECT 2;")},
		"sql/0001_bootstrap.up.sql": {Data: []byte("SELECT 1;")},
		"sql/README.md":             {Data: []byte("ignore")},
	}, "sql")
	if err != nil {
		t.Fatalf("load steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Version != "0001_bootstrap" || steps[0].Name != "bootstrap" {
		t.Fatalf("unexpected first step: %#v", steps[0])
	}
	if steps[1].Version != "0002_more" || steps[1].Name != "more" {
		t.Fatalf("unexpected second step: %#v", steps[1])
	}
}

func TestLoadStepsRejectsInvalidFilename(t *testing.T) {
	_, err := LoadSteps(fstest.MapFS{
		"sql/bootstrap.sql": {Data: []byte("SELECT 1;")},
	}, "sql")
	if err == nil {
		t.Fatal("expected invalid filename error")
	}
}

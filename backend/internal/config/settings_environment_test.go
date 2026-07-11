package config

import "testing"

func TestSettingsEnvironmentOnlyIncludesSuccessfullyParsedBooleans(t *testing.T) {
	t.Setenv("CHATAPI_MEDIA_ALLOW_SVG", "not-a-boolean")
	invalid, err := FromEnv(ModeServe, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := invalid.SettingsEnvironment("media")["allow_svg"]; ok {
		t.Fatal("invalid environment value must not make setting authoritative")
	}

	t.Setenv("CHATAPI_MEDIA_ALLOW_SVG", "false")
	valid, err := FromEnv(ModeServe, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	value, ok := valid.SettingsEnvironment("media")["allow_svg"]
	if !ok {
		t.Fatal("valid false environment value must remain authoritative")
	}
	if value != false {
		t.Fatalf("allow_svg=%v", value)
	}
}

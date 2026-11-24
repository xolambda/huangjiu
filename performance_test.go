package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritePerformanceFlagsPreservesExistingFlags(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Roblox.app")
	settingsDir := filepath.Join(app, "Contents", "MacOS", "ClientSettings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(settingsDir, "ClientAppSettings.json")
	if err := os.WriteFile(path, []byte(`{"CustomFlag":true,"FIntDebugForceMSAASamples":4}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writePerformanceFlags(app); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["CustomFlag"] != true {
		t.Fatalf("existing custom flag was lost: %#v", settings)
	}
	if settings["FIntDebugForceMSAASamples"] != float64(1) {
		t.Fatalf("MSAA samples = %#v, want 1", settings["FIntDebugForceMSAASamples"])
	}
	for key, expected := range map[string]float64{
		"FIntFRMMaxGrassDistance":              0,
		"FIntFRMMinGrassDistance":              0,
		"FIntGrassMovementReducedMotionFactor": 0,
	} {
		if settings[key] != expected {
			t.Errorf("%s = %#v, want %v", key, settings[key], expected)
		}
	}
}

func TestSetXMLSetting(t *testing.T) {
	original := `<Item><int name="FramerateCap">240</int><int name="GraphicsQualityLevel">21</int><token name="SavedQualityLevel">10</token></Item>`
	updated := setXMLSetting(original, "int", "FramerateCap", "9999")
	updated = setXMLSetting(updated, "int", "GraphicsQualityLevel", "3")
	updated = setXMLSetting(updated, "token", "SavedQualityLevel", "1")
	for _, expected := range []string{
		`<int name="FramerateCap">9999</int>`,
		`<int name="GraphicsQualityLevel">3</int>`,
		`<token name="SavedQualityLevel">1</token>`,
	} {
		if !strings.Contains(updated, expected) {
			t.Errorf("updated settings missing %q: %s", expected, updated)
		}
	}
}

func TestUpdateSavedRobloxSettings(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USER", "test-user")
	t.Setenv("ROSEMARY_PREFIX", filepath.Join(root, "prefix"))
	t.Setenv("ROSEMARY_PREFIX_HOME", "")
	settingsDir := filepath.Join(root, "prefix", "root", "Users", "test-user", "Library", "Roblox")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(settingsDir, "GlobalBasicSettings_test.xml")
	original := `<Item><int name="FramerateCap">240</int><int name="GraphicsQualityLevel">21</int><token name="SavedQualityLevel">10</token></Item>`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := updateSavedRobloxSettings(filepath.Join(root, "data"), 9999, 1); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`<int name="FramerateCap">9999</int>`,
		`<int name="GraphicsQualityLevel">3</int>`,
		`<token name="SavedQualityLevel">1</token>`,
	} {
		if !strings.Contains(string(data), expected) {
			t.Errorf("saved settings missing %q: %s", expected, data)
		}
	}
}

func TestBuildEnvPerformanceDefaults(t *testing.T) {
	t.Setenv("HUANGJIU_ADAPTIVE_VSYNC", "")
	for _, key := range []string{
		"ROSEMARY_DECORATIONS",
		"ROSEMARY_MENU",
		"ROSEMARY_SCREEN_REFRESH_RATE",
		"ROSEMARY_SWAP_INTERVAL",
		"ROSEMARY_METAL_PRESENTATION_MODE",
	} {
		t.Setenv(key, "")
	}
	env := buildEnv(t.TempDir())
	if got := envValue(env, "ROSEMARY_SCREEN_REFRESH_RATE"); got != defaultGuestRefreshRate {
		t.Errorf("ROSEMARY_SCREEN_REFRESH_RATE = %q, want %s", got, defaultGuestRefreshRate)
	}
	for key, expected := range map[string]string{
		"ROSEMARY_SWAP_INTERVAL":           "0",
		"ROSEMARY_METAL_PRESENTATION_MODE": "mailbox",
	} {
		if got := envValue(env, key); got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestBuildEnvPreservesPresentationOverrides(t *testing.T) {
	t.Setenv("ROSEMARY_SCREEN_REFRESH_RATE", "240")
	t.Setenv("ROSEMARY_SWAP_INTERVAL", "1")
	t.Setenv("ROSEMARY_METAL_PRESENTATION_MODE", "fifo")
	env := buildEnv(t.TempDir())
	for key, expected := range map[string]string{
		"ROSEMARY_SCREEN_REFRESH_RATE":     "240",
		"ROSEMARY_SWAP_INTERVAL":           "1",
		"ROSEMARY_METAL_PRESENTATION_MODE": "fifo",
	} {
		if got := envValue(env, key); got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestPerformanceProfileDefaultsOff(t *testing.T) {
	t.Setenv("HUANGJIU_PERFORMANCE", "")
	if performanceEnabled() {
		t.Fatal("performance preset must be disabled by default")
	}
}

func TestApplyPerformanceProfileDoesNotModifyClientByDefault(t *testing.T) {
	t.Setenv("HUANGJIU_PERFORMANCE", "")
	app := filepath.Join(t.TempDir(), "Roblox.app")
	settingsDir := filepath.Join(app, "Contents", "MacOS", "ClientSettings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "ClientAppSettings.json")
	original := `{"KeepThis":true}`
	if err := os.WriteFile(settingsPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := applyPerformanceProfile(app, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != original {
		t.Fatalf("client settings changed with profile disabled: %s", actual)
	}
}

func TestPerformanceProfileRequiresExplicitOptIn(t *testing.T) {
	for _, value := range []string{"1", "true", "on", "yes", "enabled"} {
		t.Setenv("HUANGJIU_PERFORMANCE", value)
		if !performanceEnabled() {
			t.Errorf("performance preset should be enabled for %q", value)
		}
	}
}

func TestPerformanceProfileRejectsOtherValues(t *testing.T) {
	for _, value := range []string{"0", "false", "off", "no", "disabled", "typo"} {
		t.Setenv("HUANGJIU_PERFORMANCE", value)
		if performanceEnabled() {
			t.Errorf("performance preset should be disabled for %q", value)
		}
	}
}

func TestBuildEnvAdaptiveVSync(t *testing.T) {
	t.Setenv("HUANGJIU_ADAPTIVE_VSYNC", "1")
	t.Setenv("ROSEMARY_SWAP_INTERVAL", "")
	t.Setenv("ROSEMARY_SCREEN_REFRESH_RATE", "")
	env := buildEnv(t.TempDir())
	if got := envValue(env, "ROSEMARY_SWAP_INTERVAL"); got != "-1" {
		t.Fatalf("adaptive swap interval = %q, want -1", got)
	}
	if got := envValue(env, "ROSEMARY_SCREEN_REFRESH_RATE"); got != defaultGuestRefreshRate {
		t.Fatalf("guest refresh override = %q, want %s", got, defaultGuestRefreshRate)
	}
}

func TestExplicitSwapIntervalOverridesAdaptiveVSync(t *testing.T) {
	t.Setenv("HUANGJIU_ADAPTIVE_VSYNC", "1")
	t.Setenv("ROSEMARY_SWAP_INTERVAL", "0")
	env := buildEnv(t.TempDir())
	if got := envValue(env, "ROSEMARY_SWAP_INTERVAL"); got != "0" {
		t.Fatalf("explicit swap interval = %q, want 0", got)
	}
}

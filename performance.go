package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var performanceFlags = map[string]any{
	"FIntDebugForceMSAASamples":            1,
	"FIntFRMMaxGrassDistance":              0,
	"FIntFRMMinGrassDistance":              0,
	"FIntGrassMovementReducedMotionFactor": 0,
}

func defaultSwapInterval() string {
	if adaptiveVSyncEnabled() {
		return "-1"
	}
	return "0"
}

func adaptiveVSyncEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HUANGJIU_ADAPTIVE_VSYNC"))) {
	case "1", "true", "on", "yes", "enabled":
		return true
	default:
		return false
	}
}

func performanceEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("HUANGJIU_PERFORMANCE")))
	switch value {
	case "1", "true", "on", "yes", "enabled":
		return true
	default:
		return false
	}
}

func applyPerformanceProfile(appPath, dataDir string) error {
	if !performanceEnabled() {
		return nil
	}
	if err := writePerformanceFlags(appPath); err != nil {
		return fmt.Errorf("write Roblox performance flags: %w", err)
	}
	cap, err := intEnv("HUANGJIU_FRAMERATE_CAP", 9999, 0, 100000)
	if err != nil {
		return err
	}
	quality, err := intEnv("HUANGJIU_GRAPHICS_QUALITY", 1, 0, 10)
	if err != nil {
		return err
	}
	if cap == 0 && quality == 0 {
		return nil
	}
	return updateSavedRobloxSettings(dataDir, cap, quality)
}

func intEnv(name string, fallback, min, max int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, min, max)
	}
	return n, nil
}

func writePerformanceFlags(appPath string) error {
	settingsDir := filepath.Join(appPath, "Contents", "MacOS", "ClientSettings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(settingsDir, "ClientAppSettings.json")
	settings := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("existing %s is not valid JSON: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for key, value := range performanceFlags {
		settings[key] = value
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func guestRobloxSettingsDir(dataDir string) (string, error) {
	if strings.EqualFold(os.Getenv("ROSEMARY_PREFIX_HOME"), "host") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Roblox"), nil
	}
	prefix := os.Getenv("ROSEMARY_PREFIX")
	if prefix == "" {
		prefix = filepath.Join(dataDir, "prefix")
	}
	if prefix == "off" {
		return "", nil
	}
	username := os.Getenv("USER")
	if username == "" {
		current, err := user.Current()
		if err != nil {
			return "", err
		}
		username = current.Username
	}
	return filepath.Join(prefix, "root", "Users", username, "Library", "Roblox"), nil
}

func updateSavedRobloxSettings(dataDir string, frameCap, quality int) error {
	dir, err := guestRobloxSettingsDir(dataDir)
	if err != nil || dir == "" {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "GlobalBasicSettings_*.xml"))
	if err != nil {
		return err
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := string(data)
		if frameCap > 0 {
			updated = setXMLSetting(updated, "int", "FramerateCap", strconv.Itoa(frameCap))
		}
		if quality > 0 {
			level := (quality*21 + 9) / 10
			if level > 21 {
				level = 21
			}
			updated = setXMLSetting(updated, "token", "SavedQualityLevel", strconv.Itoa(quality))
			updated = setXMLSetting(updated, "int", "GraphicsQualityLevel", strconv.Itoa(level))
		}
		if updated != string(data) {
			if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func setXMLSetting(data, tag, name, value string) string {
	pattern := regexp.MustCompile(`(<` + regexp.QuoteMeta(tag) + ` name="` + regexp.QuoteMeta(name) + `">)[^<]*(</` + regexp.QuoteMeta(tag) + `>)`)
	return pattern.ReplaceAllString(data, "${1}"+value+"${2}")
}

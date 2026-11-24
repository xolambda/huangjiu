package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZipEntry(t *testing.T, zw *zip.Writer, name, body string, mode os.FileMode) {
	t.Helper()
	header := &zip.FileHeader{Name: name}
	header.SetMode(mode)
	w, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
}

func TestSafeZipPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../outside", "../../etc/passwd", "/tmp/escape"} {
		if _, err := safeZipPath(root, name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestExtractZipCreatesAppBundle(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "client.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	writeZipEntry(t, zw, "Roblox.app/", "", os.ModeDir|0755)
	writeZipEntry(t, zw, "Roblox.app/Contents/", "", os.ModeDir|0755)
	writeZipEntry(t, zw, "Roblox.app/Contents/MacOS/", "", os.ModeDir|0755)
	writeZipEntry(t, zw, "Roblox.app/Contents/MacOS/RobloxPlayer", "binary", 0755)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(root, "out")
	if err := extractZip(archive, dest); err != nil {
		t.Fatal(err)
	}
	app, err := findExtractedApp(dest)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(app, "Contents", "MacOS", "RobloxPlayer")
	if _, err := os.Stat(binary); err != nil {
		t.Fatal(err)
	}
}

func TestExtractZipRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "bad.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	writeZipEntry(t, zw, "../escaped", "nope", 0644)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractZip(archive, filepath.Join(root, "out")); err == nil {
		t.Fatal("expected path traversal archive to be rejected")
	}
	if _, err := os.Stat(filepath.Join(root, "escaped")); !os.IsNotExist(err) {
		t.Fatal("archive wrote outside destination")
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func TestBuildEnvUsesDefaults(t *testing.T) {
	t.Setenv("ROSEMARY_DECORATIONS", "")
	t.Setenv("ROSEMARY_MENU", "")
	t.Setenv("ROSEMARY_SCREEN_REFRESH_RATE", "")
	t.Setenv("HUANGJIU_ADAPTIVE_VSYNC", "")
	env := buildEnv(t.TempDir())
	if got := envValue(env, "ROSEMARY_DECORATIONS"); got != "server" {
		t.Fatalf("ROSEMARY_DECORATIONS = %q, want server", got)
	}
	if got := envValue(env, "ROSEMARY_MENU"); got != "none" {
		t.Fatalf("ROSEMARY_MENU = %q, want none", got)
	}
	if got := envValue(env, "ROSEMARY_SCREEN_REFRESH_RATE"); got != defaultGuestRefreshRate {
		t.Fatalf("ROSEMARY_SCREEN_REFRESH_RATE = %q, want %s", got, defaultGuestRefreshRate)
	}
	if got := envValue(env, "FFLAG_DFIntTaskSchedulerTargetFps"); got != "" {
		t.Fatalf("unexpected host FFlag environment variable: %q", got)
	}
}

func TestBuildEnvPreservesRosemaryOverrides(t *testing.T) {
	t.Setenv("ROSEMARY_DECORATIONS", "client")
	t.Setenv("ROSEMARY_MENU", "none")
	t.Setenv("ROSEMARY_SCREEN_REFRESH_RATE", "144")
	env := buildEnv(t.TempDir())
	if got := envValue(env, "ROSEMARY_DECORATIONS"); got != "client" {
		t.Fatalf("ROSEMARY_DECORATIONS = %q, want client", got)
	}
	if got := envValue(env, "ROSEMARY_MENU"); got != "none" {
		t.Fatalf("ROSEMARY_MENU = %q, want none", got)
	}
	if got := envValue(env, "ROSEMARY_SCREEN_REFRESH_RATE"); got != "144" {
		t.Fatalf("ROSEMARY_SCREEN_REFRESH_RATE = %q, want 144", got)
	}
}

package main

/*
#cgo CFLAGS: -I${SRCDIR}/native
#cgo LDFLAGS: -L${SRCDIR}/native -lhuangjiu_core
#include <stdlib.h>
#include "core.h"
*/
import "C"

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const defaultRosemaryPath = "/usr/local/bin/rosemary"
const defaultGuestRefreshRate = "240"

type sysinfo struct {
	TotalMemBytes uint64
	FreeMemBytes  uint64
	PageSize      uint64
	NCPU          int
	HWModel       string
	OSRelease     string
}

func getSysinfo() (sysinfo, error) {
	var raw C.huangjiu_sysinfo
	if C.huangjiu_get_sysinfo(&raw) != 0 {
		return sysinfo{}, errors.New("sysinfo unavailable on this platform")
	}
	return sysinfo{
		TotalMemBytes: uint64(raw.total_memory_bytes),
		FreeMemBytes:  uint64(raw.free_memory_bytes),
		PageSize:      uint64(raw.page_size),
		NCPU:          int(raw.ncpu),
		HWModel:       C.GoString(&raw.hw_model[0]),
		OSRelease:     C.GoString(&raw.os_release[0]),
	}, nil
}

func isExecutable(path string) bool {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	return C.huangjiu_is_executable(cpath) == 1
}

func dataDirectory() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "huangjiu"), nil
}

func validAppBundle(app string) bool {
	macOSDir := filepath.Join(app, "Contents", "MacOS")
	entries, err := os.ReadDir(macOSDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() || entry.Type()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

func findRobloxApp(dataDir string) (string, error) {
	clientsDir := filepath.Join(dataDir, "clients")
	entries, err := os.ReadDir(clientsDir)
	if err != nil {
		return "", fmt.Errorf("no client installed yet (looked in %s): %w", clientsDir, err)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if !entries[i].IsDir() {
			continue
		}
		app := filepath.Join(clientsDir, entries[i].Name(), "Roblox.app")
		if validAppBundle(app) {
			return app, nil
		}
	}
	return "", fmt.Errorf("no valid Roblox.app found under %s", clientsDir)
}

func buildEnv(dataDir string) []string {
	env := os.Environ()
	set := func(key, value string) {
		prefix := key + "="
		for i, item := range env {
			if strings.HasPrefix(item, prefix) {
				env[i] = prefix + value
				return
			}
		}
		env = append(env, prefix+value)
	}
	if os.Getenv("ROSEMARY_PREFIX") == "" {
		set("ROSEMARY_PREFIX", filepath.Join(dataDir, "prefix"))
	}
	if os.Getenv("ROSEMARY_DECORATIONS") == "" {
		set("ROSEMARY_DECORATIONS", "server")
	}
	if os.Getenv("ROSEMARY_MENU") == "" {
		set("ROSEMARY_MENU", "none")
	}
	if os.Getenv("ROSEMARY_SCREEN_REFRESH_RATE") == "" {
		set("ROSEMARY_SCREEN_REFRESH_RATE", defaultGuestRefreshRate)
	}
	if os.Getenv("ROSEMARY_SWAP_INTERVAL") == "" {
		set("ROSEMARY_SWAP_INTERVAL", defaultSwapInterval())
	}
	if os.Getenv("ROSEMARY_METAL_PRESENTATION_MODE") == "" {
		set("ROSEMARY_METAL_PRESENTATION_MODE", "mailbox")
	}
	return env
}

func run(ctx context.Context, rosemaryPath, appPath, gameLink string, env []string) error {
	args := []string{"run", appPath}
	if gameLink != "" {
		args = append(args, "-protocolString", gameLink)
	}
	cmd := exec.Command(rosemaryPath, args...)
	cmd.Env, cmd.Stdout, cmd.Stderr, cmd.Stdin = env, os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start Rosemary: %w", err)
	}
	log.Printf("huangjiu: Rosemary started (pid %d)", cmd.Process.Pid)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case err := <-done:
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					return nil
				}
			}
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		return nil
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Rosemary exited: %w", err)
		}
		return nil
	}
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func packageInstalled(name string) bool {
	if !commandExists("pkg") {
		return false
	}
	return exec.Command("pkg", "info", "-e", name).Run() == nil
}

func doctor(dataDir, rosemaryPath string) {
	fmt.Println("Huangjiu diagnostics")
	fmt.Printf("  Data directory: %s\n", dataDir)
	fmt.Printf("  Rosemary:       %s\n", rosemaryPath)
	if isExecutable(rosemaryPath) {
		fmt.Println("  [ok] Rosemary executable")
	} else {
		fmt.Println("  [!!] Rosemary missing or not executable")
	}
	fmt.Printf("  [..] Window decorations: %s\n", envOr("ROSEMARY_DECORATIONS", "server"))
	fmt.Printf("  [..] Rosemary menu:      %s\n", envOr("ROSEMARY_MENU", "none"))
	fmt.Printf("  [..] Guest refresh rate: %s\n", envOr("ROSEMARY_SCREEN_REFRESH_RATE", defaultGuestRefreshRate+" Hz"))
	fmt.Printf("  [..] Swap interval:      %s\n", envOr("ROSEMARY_SWAP_INTERVAL", defaultSwapInterval()))
	fmt.Printf("  [..] Metal present mode: %s\n", envOr("ROSEMARY_METAL_PRESENTATION_MODE", "mailbox"))
	fmt.Printf("  [..] Performance preset: %t\n", performanceEnabled())
	if commandExists("fc-match") {
		out, err := exec.Command("fc-match", "sans").Output()
		if err == nil && len(strings.TrimSpace(string(out))) > 0 {
			fmt.Printf("  [ok] Fontconfig found a sans font: %s\n", strings.TrimSpace(string(out)))
		} else {
			fmt.Println("  [!!] Fontconfig is installed but could not find a sans font")
		}
	} else {
		fmt.Println("  [!!] fc-match not found; install fontconfig and a font package (e.g. dejavu)")
	}
	if packageInstalled("dejavu") {
		fmt.Println("  [ok] FreeBSD dejavu package installed")
	} else {
		fmt.Println("  [!!] FreeBSD dejavu package not detected (install with: pkg install dejavu)")
	}
	if packageInstalled("mesa-dri") {
		fmt.Println("  [ok] FreeBSD mesa-dri package installed")
	} else {
		fmt.Println("  [!!] FreeBSD mesa-dri package not detected; AMD/Intel Vulkan support may be incomplete")
	}
	if commandExists("vulkaninfo") {
		fmt.Println("  [ok] vulkaninfo available")
	} else {
		fmt.Println("  [!!] vulkaninfo not found; install Vulkan tools to inspect the driver")
	}
	if v := os.Getenv("VK_DRIVER_FILES"); v != "" {
		fmt.Printf("  [..] VK_DRIVER_FILES=%s\n", v)
	}
	fmt.Println()
	fmt.Println("Note: these checks can identify missing system components; they cannot verify that every Roblox Metal rendering feature translates correctly through Rosemary.")
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help") {
		fmt.Println("huangjiu [launch [roblox-link] | update | status | doctor]\n\nCommands:\n  launch [link]  update if needed and launch Roblox\n  update         download the latest Roblox client\n  status         show installation status\n  doctor         check Rosemary, fonts, and graphics prerequisites")
		return
	}
	command := "launch"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("huangjiu", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: huangjiu [launch [roblox-link] | update | status | doctor]") }
	_ = fs.Parse(args)
	if command != "launch" && command != "update" && command != "status" && command != "doctor" {
		fs.Usage()
		os.Exit(2)
	}
	gameLink := ""
	if command == "launch" && fs.NArg() > 0 {
		gameLink = fs.Arg(0)
	}

	si, sysErr := getSysinfo()
	if sysErr != nil {
		log.Printf("huangjiu: warning: %v", sysErr)
	} else {
		log.Printf("huangjiu: %s on FreeBSD %s, %d CPUs, %d MiB RAM", si.HWModel, si.OSRelease, si.NCPU, si.TotalMemBytes/(1024*1024))
	}
	dataDir, err := dataDirectory()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatal(err)
	}
	f := newFetcher(dataDir)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	rosemary := os.Getenv("HUANGJIU_ROSEMARY")
	if rosemary == "" {
		rosemary = defaultRosemaryPath
	}
	if command == "doctor" {
		doctor(dataDir, rosemary)
		return
	}
	if command == "status" {
		fmt.Printf("Data directory: %s\n", dataDir)
		if p, err := findRobloxApp(dataDir); err == nil {
			fmt.Printf("Roblox client:  %s\n", p)
		} else {
			fmt.Printf("Roblox client:  not installed (%v)\n", err)
		}
		fmt.Printf("Rosemary:       %s\n", rosemary)
		fmt.Printf("Decorations:    %s\n", envOr("ROSEMARY_DECORATIONS", "server"))
		fmt.Printf("Menu:           %s\n", envOr("ROSEMARY_MENU", "none"))
		fmt.Printf("Guest refresh:  %s\n", envOr("ROSEMARY_SCREEN_REFRESH_RATE", defaultGuestRefreshRate+" Hz"))
		fmt.Printf("Swap interval:  %s\n", envOr("ROSEMARY_SWAP_INTERVAL", defaultSwapInterval()))
		fmt.Printf("Present mode:   %s\n", envOr("ROSEMARY_METAL_PRESENTATION_MODE", "mailbox"))
		fmt.Printf("Performance:    %t\n", performanceEnabled())
		if isExecutable(rosemary) {
			fmt.Println("Rosemary status: executable")
		} else {
			fmt.Println("Rosemary status: missing or not executable")
		}
		return
	}
	installedPath, installedErr := findRobloxApp(dataDir)
	appPath, updateErr := f.fetch(ctx)
	if updateErr != nil {
		if command == "update" || installedErr != nil {
			log.Fatalf("huangjiu: client update failed: %v", updateErr)
		}
		log.Printf("huangjiu: update check failed; using installed client: %v", updateErr)
		appPath = installedPath
	}
	if command == "update" {
		log.Printf("huangjiu: client installed at %s", appPath)
		return
	}
	if err := applyPerformanceProfile(appPath, dataDir); err != nil {
		log.Printf("huangjiu: performance profile warning: %v", err)
	}
	if !isExecutable(rosemary) {
		log.Fatalf("huangjiu: Rosemary not found or not executable at %s (set HUANGJIU_ROSEMARY)", rosemary)
	}
	if err := run(ctx, rosemary, appPath, gameLink, buildEnv(dataDir)); err != nil {
		log.Fatal(err)
	}
	log.Println("huangjiu: session ended")
}

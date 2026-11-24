package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const clientVersionURL = "https://clientsettingscdn.roblox.com/v2/client-version/MacPlayer/channel/LIVE"

var versionHashPattern = regexp.MustCompile(`^version-[A-Za-z0-9]+$`)

type clientVersion struct {
	Version             string `json:"version"`
	ClientVersionUpload string `json:"clientVersionUpload"`
}

type fetcher struct {
	dataDir string
	client  *http.Client
}

func newFetcher(dataDir string) *fetcher {
	return &fetcher{dataDir: dataDir, client: &http.Client{Timeout: 2 * time.Minute}}
}

func (f *fetcher) latestVersion(ctx context.Context) (clientVersion, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientVersionURL, nil)
	if err != nil {
		return clientVersion{}, err
	}
	req.Header.Set("User-Agent", "huangjiu/0.2 (+FreeBSD)")
	resp, err := f.client.Do(req)
	if err != nil {
		return clientVersion{}, fmt.Errorf("version check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return clientVersion{}, fmt.Errorf("version endpoint returned HTTP %s", resp.Status)
	}
	var cv clientVersion
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cv); err != nil {
		return clientVersion{}, fmt.Errorf("invalid version response: %w", err)
	}
	if !versionHashPattern.MatchString(cv.ClientVersionUpload) {
		return clientVersion{}, fmt.Errorf("version response contains an invalid clientVersionUpload")
	}
	return cv, nil
}

func (f *fetcher) downloadZIP(ctx context.Context, hash, dest string) error {
	filename := hash + "-RobloxPlayer.zip"
	var failures []string
	for _, base := range []string{"https://setup.rbxcdn.com/mac/", "https://setup-aws.rbxcdn.com/mac/", "https://setup-ak.rbxcdn.com/mac/"} {
		url := base + filename
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "huangjiu/0.2 (+FreeBSD)")
		resp, err := f.client.Do(req)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if resp.StatusCode != http.StatusOK {
			failures = append(failures, fmt.Sprintf("%s: HTTP %s", base, resp.Status))
			resp.Body.Close()
			continue
		}
		tmp := dest + ".part"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			resp.Body.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(resp.Body, 2<<30))
		bodyErr := resp.Body.Close()
		closeErr := out.Close()
		if copyErr != nil || bodyErr != nil || closeErr != nil {
			_ = os.Remove(tmp)
			return errors.Join(copyErr, bodyErr, closeErr)
		}
		if err := os.Rename(tmp, dest); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	}
	return fmt.Errorf("could not download Roblox client: %s", strings.Join(failures, "; "))
}

func safeZipPath(root, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("absolute path in archive: %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	full := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive path escapes destination: %q", name)
	}
	return full, nil
}

func ensureNoSymlinkParents(root, target string) error {
	rel, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil {
		return err
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return fmt.Errorf("archive attempts to write through non-directory %s", current)
		}
	}
	return nil
}

func extractZip(zipPath, destination string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open Roblox ZIP: %w", err)
	}
	defer zr.Close()
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	var symlinks []*zip.File
	for _, file := range zr.File {
		target, err := safeZipPath(root, file.Name)
		if err != nil {
			return err
		}
		mode := file.Mode()
		if mode&os.ModeSymlink != 0 {
			symlinks = append(symlinks, file)
			continue
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() {
			continue
		}
		if err := ensureNoSymlinkParents(root, target); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		r, err := file.Open()
		if err != nil {
			return err
		}
		perm := mode.Perm() & 0755
		if perm == 0 {
			perm = 0644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
		if err != nil {
			r.Close()
			return err
		}
		_, copyErr := io.Copy(out, r)
		readCloseErr := r.Close()
		writeCloseErr := out.Close()
		if err := errors.Join(copyErr, readCloseErr, writeCloseErr); err != nil {
			return err
		}
		if err := os.Chmod(target, perm); err != nil {
			return err
		}
	}
	for _, file := range symlinks {
		target, err := safeZipPath(root, file.Name)
		if err != nil {
			return err
		}
		if err := ensureNoSymlinkParents(root, target); err != nil {
			return err
		}
		r, err := file.Open()
		if err != nil {
			return err
		}
		linkBytes, readErr := io.ReadAll(io.LimitReader(r, 1<<16))
		closeErr := r.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		link := string(linkBytes)
		if link == "" || filepath.IsAbs(link) {
			return fmt.Errorf("unsafe symlink target in %s", file.Name)
		}
		resolved := filepath.Clean(filepath.Join(filepath.Dir(target), filepath.FromSlash(link)))
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("symlink escapes install directory: %s", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("duplicate archive entry: %s", file.Name)
		}
		if err := os.Symlink(link, target); err != nil {
			return err
		}
	}
	return nil
}

func fixBundlePerms(appPath string) error {
	macOSDir := filepath.Join(appPath, "Contents", "MacOS")
	return filepath.WalkDir(macOSDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		return os.Chmod(path, 0755)
	})
}

func findExtractedApp(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".app") {
			if st, statErr := os.Stat(filepath.Join(path, "Contents", "MacOS")); statErr == nil && st.IsDir() {
				found = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("archive contains no valid Roblox.app")
	}
	return found, nil
}

func (f *fetcher) fetch(ctx context.Context) (string, error) {
	cv, err := f.latestVersion(ctx)
	if err != nil {
		return "", err
	}
	log.Printf("huangjiu: Roblox version %s (%s)", cv.Version, cv.ClientVersionUpload)
	targetDir := filepath.Join(f.dataDir, "clients", cv.ClientVersionUpload)
	appPath := filepath.Join(targetDir, "Roblox.app")
	if validAppBundle(appPath) {
		log.Printf("huangjiu: build already installed")
		return appPath, nil
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", err
	}
	zipPath := filepath.Join(targetDir, "roblox-player.zip")
	log.Printf("huangjiu: downloading Roblox client")
	if err := f.downloadZIP(ctx, cv.ClientVersionUpload, zipPath); err != nil {
		return "", err
	}
	defer os.Remove(zipPath)
	log.Printf("huangjiu: extracting Roblox client")
	unpackDir := filepath.Join(targetDir, "unpack")
	if err := os.RemoveAll(unpackDir); err != nil {
		return "", err
	}
	defer os.RemoveAll(unpackDir)
	if err := extractZip(zipPath, unpackDir); err != nil {
		return "", fmt.Errorf("extract client: %w", err)
	}
	extractedApp, err := findExtractedApp(unpackDir)
	if err != nil {
		return "", err
	}
	if err := os.Rename(extractedApp, appPath); err != nil {
		return "", fmt.Errorf("install app bundle: %w", err)
	}
	if err := fixBundlePerms(appPath); err != nil {
		return "", fmt.Errorf("fix client permissions: %w", err)
	}
	return appPath, nil
}

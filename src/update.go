package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func assetNameForCurrentPlatform() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "tiny_windows_amd64.exe", nil
		}
	case "linux":
		if runtime.GOARCH == "amd64" {
			return "tiny_linux_amd64", nil
		}

		if runtime.GOARCH == "arm64" {
			return "tiny_linux_arm64", nil
		}
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "tiny_darwin_arm64", nil
		}
	}

	return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func findAsset(release Release, name string) (Asset, error) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, nil
		}
	}

	return Asset{}, fmt.Errorf("release asset not found: %s", name)
}

func downloadFile(url, dst string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func replaceCurrentExecutableUnix(newPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return err
	}

	if err := os.Chmod(newPath, 0755); err != nil {
		return err
	}

	backupPath := exePath + ".old"

	_ = os.Remove(backupPath)

	if err := os.Rename(exePath, backupPath); err != nil {
		return err
	}

	if err := os.Rename(newPath, exePath); err != nil {
		_ = os.Rename(backupPath, exePath)
		return err
	}

	_ = os.Remove(backupPath)
	return nil
}

func replaceCurrentExecutableWindows(newPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	pid := os.Getpid()

	script := fmt.Sprintf(`
$ErrorActionPreference = "SilentlyContinue"

$pidToWait = %d
$src = %q
$dst = %q

$p = Get-Process -Id $pidToWait -ErrorAction SilentlyContinue
if ($p) {
    Wait-Process -Id $pidToWait -ErrorAction SilentlyContinue
}

Start-Sleep -Milliseconds 500

for ($i = 0; $i -lt 30; $i++) {
    try {
        Move-Item -LiteralPath $src -Destination $dst -Force -ErrorAction Stop
        exit 0
    } catch {
        Start-Sleep -Milliseconds 500
    }
}

exit 1
`, pid, newPath, exePath)

	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-Command", script,
	)

	return cmd.Start()
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

func compareVersions(current, latest string) (int, error) {
	currentParts := strings.Split(normalizeVersion(current), ".")
	latestParts := strings.Split(normalizeVersion(latest), ".")

	maxLen := len(currentParts)
	if len(latestParts) > maxLen {
		maxLen = len(latestParts)
	}

	for i := 0; i < maxLen; i++ {
		var currentNum, latestNum int

		if i < len(currentParts) {
			n, err := strconv.Atoi(currentParts[i])
			if err != nil {
				return 0, err
			}
			currentNum = n
		}

		if i < len(latestParts) {
			n, err := strconv.Atoi(latestParts[i])
			if err != nil {
				return 0, err
			}
			latestNum = n
		}

		if currentNum < latestNum {
			return -1, nil
		}
		if currentNum > latestNum {
			return 1, nil
		}
	}

	return 0, nil
}

func shouldUpdate(current, latest string) (bool, error) {
	cmp, err := compareVersions(current, latest)
	if err != nil {
		return false, err
	}

	return cmp < 0, nil
}

func latestRelease(owner, repo string) (Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}

	req.Header.Set("User-Agent", "tiny-updater")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github release lookup failed: %s", resp.Status)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, err
	}

	if release.TagName == "" {
		return Release{}, fmt.Errorf("latest release has no tag_name")
	}

	return release, nil
}

func installDownloadedUpdate(downloadURL string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	dir := filepath.Dir(exePath)
	currentName := filepath.Base(exePath)

	newPath := filepath.Join(dir, currentName+".update")

	if err := downloadFile(downloadURL, newPath); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		if err := replaceCurrentExecutableWindows(newPath); err != nil {
			return err
		}

		fmt.Println("Update downloaded. Tiny will be replaced after this process exits.")
		os.Exit(0)
	}

	return replaceCurrentExecutableUnix(newPath)
}

func updateCommand() {
	release, err := latestRelease("confh", "Tiny")
	if err != nil {
		panic(err)
	}

	update, err := shouldUpdate(TinyVersion, release.TagName)
	if err != nil {
		panic(err)
	}

	if !update {
		fmt.Printf("Already up to date: %s\n", TinyVersion)
		return
	}

	assetName, err := assetNameForCurrentPlatform()
	if err != nil {
		panic(err)
	}

	asset, err := findAsset(release, assetName)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Updating Tiny %s -> %s\n", TinyVersion, release.TagName)
	fmt.Printf("Downloading %s...\n", asset.Name)

	if err := installDownloadedUpdate(asset.URL); err != nil {
		panic(err)
	}

	fmt.Println("Update installed successfully.")
}

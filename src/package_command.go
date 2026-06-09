package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	. "language.com/src/tinyerrors"
)

type githubPackageSpec struct {
	Owner string
	Repo  string
	Ref   string
}

type githubRepoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

type githubReleaseInfo struct {
	Assets []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubContentInfo struct {
	Type     string `json:"type"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

func addPackageCommand(args []string) {
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny add <github:owner/repo[@ref]> or tiny add <name> <github:owner/repo[@ref]>")
	}

	source := args[0]
	name := ""

	if len(args) >= 2 {
		name = args[0]
		source = args[1]
	}

	spec := parseGitHubPackageSource(source)
	if name == "" {
		name = spec.Repo
	}

	config, ok := loadTinyConfig()
	if !ok {
		LangError(ErrorRuntime, "tiny.json not found")
	}

	if config.Dependencies == nil {
		config.Dependencies = map[string]TinyDependencyConfig{}
	}

	dep := TinyDependencyConfig{
		Source:  canonicalGitHubSource(spec),
		Version: spec.Ref,
	}

	dep = installOneDependency(name, dep, config.Target)

	config.Dependencies[name] = dep
	writeJSONFile("tiny.json", config)
}

func installPackagesCommand(args []string) {
	target := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			if i+1 >= len(args) {
				LangError(ErrorRuntime, "expected target after --target")
			}
			target = normalizeTarget(args[i+1])
			i++

		default:
			LangError(ErrorRuntime, "unknown install argument: %s", args[i])
		}
	}

	config, ok := loadTinyConfig()
	if !ok {
		LangError(ErrorRuntime, "tiny.json not found")
	}

	if target == "" {
		target = config.Target
	}

	if len(config.Dependencies) == 0 {
		fmt.Println("No dependencies in tiny.json")
		return
	}

	for name, dep := range config.Dependencies {
		installOneDependency(name, dep, target)
	}
}

func removePackageCommand(args []string) {
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny remove <name|owner/repo> [--global|--project-only]")
	}

	target := ""
	removeGlobal := true
	globalOnly := false

	for _, arg := range args {
		switch arg {
		case "--project-only":
			removeGlobal = false
		case "--global":
			globalOnly = true
		default:
			if target != "" {
				LangError(ErrorRuntime, "unknown remove argument: %s", arg)
			}
			target = arg
		}
	}

	if target == "" {
		LangError(ErrorRuntime, "usage: tiny remove <name|owner/repo> [--global|--project-only]")
	}

	removed := []TinyDependencyConfig{}
	if !globalOnly {
		config, ok := loadTinyConfig()
		if !ok {
			LangError(ErrorRuntime, "tiny.json not found")
		}

		if len(config.Dependencies) == 0 {
			LangError(ErrorRuntime, "no dependencies in tiny.json")
		}

		next := map[string]TinyDependencyConfig{}
		for name, dep := range config.Dependencies {
			if dependencyMatchesRemoveTarget(name, dep, target) {
				removed = append(removed, dep)
				continue
			}
			next[name] = dep
		}

		if len(removed) == 0 {
			LangError(ErrorRuntime, "dependency not found: %s", target)
		}

		config.Dependencies = next
		writeJSONFile("tiny.json", config)
	}

	if globalOnly {
		spec := parseGitHubPackageSource(target)
		if err := removeInstalledLibrary(spec.Owner, spec.Repo, spec.Ref); err != nil {
			LangError(ErrorRuntime, "failed to remove dependency %s: %v", target, err)
		}
		fmt.Println("Removed downloaded dependency:", spec.Owner+"/"+spec.Repo)
		return
	}

	if removeGlobal {
		for _, dep := range removed {
			if dep.Source == "" {
				continue
			}
			spec := parseGitHubPackageSource(dep.Source)
			version := dep.Version
			if version == "" {
				version = spec.Ref
			}
			if err := removeInstalledLibrary(spec.Owner, spec.Repo, version); err != nil {
				LangError(ErrorRuntime, "failed to remove downloaded dependency %s/%s: %v", spec.Owner, spec.Repo, err)
			}
		}
	}

	fmt.Println("Removed dependency:", target)
}

func listDownloadedDependenciesCommand(args []string) {
	if len(args) > 0 {
		LangError(ErrorRuntime, "usage: tiny deps")
	}

	libs := scanInstalledLibraries()
	if len(libs) == 0 {
		fmt.Println("No downloaded dependencies")
		return
	}

	for _, lib := range libs {
		if len(lib.Versions) == 0 {
			fmt.Println(lib.Owner + "/" + lib.Repo)
			continue
		}
		fmt.Printf("%s/%s @ %s\n", lib.Owner, lib.Repo, strings.Join(lib.Versions, ", "))
	}
}

func dependencyMatchesRemoveTarget(name string, dep TinyDependencyConfig, target string) bool {
	if name == target {
		return true
	}

	if dep.Source == "" || !looksLikeGitHubPackageSource(target) {
		return false
	}

	depSpec := parseGitHubPackageSource(dep.Source)
	targetSpec := parseGitHubPackageSource(target)
	if depSpec.Owner != targetSpec.Owner || depSpec.Repo != targetSpec.Repo {
		return false
	}

	return targetSpec.Ref == "" || targetSpec.Ref == dep.Version || targetSpec.Ref == depSpec.Ref
}

func looksLikeGitHubPackageSource(target string) bool {
	target = strings.TrimSpace(target)
	return strings.Contains(target, "/") || strings.HasPrefix(target, "github:") || strings.Contains(target, "github.com/")
}

func installOneDependency(name string, dep TinyDependencyConfig, target string) TinyDependencyConfig {
	if name == "" {
		LangError(ErrorRuntime, "dependency name cannot be empty")
	}

	spec := parseGitHubPackageSource(dep.Source)
	if dep.Version != "" {
		spec.Ref = dep.Version
	}

	if spec.Ref == "" {
		spec.Ref = fetchDefaultBranch(spec)
	}
	dep.Version = spec.Ref
	dep.Source = canonicalGitHubSource(githubPackageSpec{
		Owner: spec.Owner,
		Repo:  spec.Repo,
	})
	dep.Path = ""

	if target == "" {
		target = normalizeTarget("")
	} else {
		target = normalizeTarget(target)
	}

	dest := libraryGlobalRoot(spec.Owner, spec.Repo, spec.Ref)

	fmt.Printf("Installing %s from %s@%s\n", name, spec.Owner+"/"+spec.Repo, spec.Ref)

	packageConfig := fetchGitHubTinyConfig(spec)
	archiveBytes := downloadGitHubArchive(spec)
	tempDir, err := os.MkdirTemp("", "tinydep-"+name+"-")
	if err != nil {
		LangError(ErrorRuntime, "failed to create temporary dependency folder: %v", err)
	}
	defer os.RemoveAll(tempDir)

	unpackGitHubArchive(archiveBytes, tempDir)

	if !fileExists(filepath.Join(tempDir, "tiny.json")) {
		LangError(ErrorRuntime, "github package %s/%s does not contain tiny.json at the repository root", spec.Owner, spec.Repo)
	}

	installPackagePlugins(spec, packageConfig, tempDir, target)

	err = os.RemoveAll(dest)
	if err != nil {
		LangError(ErrorRuntime, "failed to replace dependency folder %s: %v", dest, err)
	}

	err = copyDirectory(tempDir, dest)
	if err != nil {
		LangError(ErrorRuntime, "failed to install dependency %s: %v", name, err)
	}

	invalidateInstalledLibraryImportCache()

	return dep
}

func installPackagePlugins(spec githubPackageSpec, config TinyProjectConfig, packageDir string, target string) {
	if len(config.Plugins) == 0 {
		return
	}

	release := fetchLatestGitHubRelease(spec)

	for _, plugin := range config.Plugins {
		if plugin.Path == "" {
			LangError(ErrorRuntime, "plugin %s in %s/%s tiny.json is missing path", plugin.Name, spec.Owner, spec.Repo)
		}

		pluginPath := normalizePluginPathForTarget(plugin.Path, target)
		downloadReleaseAssetTo(release, filepath.Base(pluginPath), filepath.Join(packageDir, pluginPath), spec)

		for _, file := range plugin.Files {
			if file == "" {
				continue
			}
			downloadReleaseAssetTo(release, filepath.Base(file), filepath.Join(packageDir, file), spec)
		}
	}
}

func parseGitHubPackageSource(source string) githubPackageSpec {
	source = strings.TrimSpace(source)
	source = strings.TrimPrefix(source, "https://")
	source = strings.TrimPrefix(source, "http://")
	source = strings.TrimPrefix(source, "github.com/")
	source = strings.TrimPrefix(source, "github:")
	source = strings.TrimSuffix(source, ".git")

	ref := ""
	if at := strings.LastIndex(source, "@"); at >= 0 {
		ref = source[at+1:]
		source = source[:at]
	}
	source = strings.TrimSuffix(source, ".git")

	parts := strings.Split(source, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		LangError(ErrorRuntime, "expected GitHub source in the form github:owner/repo[@ref]")
	}

	return githubPackageSpec{
		Owner: parts[0],
		Repo:  parts[1],
		Ref:   ref,
	}
}

func canonicalGitHubSource(spec githubPackageSpec) string {
	source := "github:" + spec.Owner + "/" + spec.Repo
	if spec.Ref != "" {
		source += "@" + spec.Ref
	}
	return source
}

func fetchDefaultBranch(spec githubPackageSpec) string {
	var info githubRepoInfo
	getJSON(githubAPIURL(spec, ""), &info)
	if info.DefaultBranch == "" {
		LangError(ErrorRuntime, "github repo %s/%s did not report a default branch", spec.Owner, spec.Repo)
	}
	return info.DefaultBranch
}

func fetchLatestGitHubRelease(spec githubPackageSpec) githubReleaseInfo {
	var release githubReleaseInfo
	getJSON(githubAPIURL(spec, "releases/latest"), &release)
	return release
}

func fetchGitHubTinyConfig(spec githubPackageSpec) TinyProjectConfig {
	var content githubContentInfo
	contentsURL := githubAPIURL(spec, "contents/tiny.json") + "?ref=" + url.QueryEscape(spec.Ref)
	getJSON(contentsURL, &content)

	if content.Type != "file" || content.Encoding != "base64" {
		LangError(ErrorRuntime, "github package %s/%s tiny.json is not a regular base64 file", spec.Owner, spec.Repo)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		LangError(ErrorRuntime, "failed to decode tiny.json from %s/%s: %v", spec.Owner, spec.Repo, err)
	}

	var config TinyProjectConfig
	err = json.Unmarshal(raw, &config)
	if err != nil {
		LangError(ErrorRuntime, "failed to parse tiny.json from %s/%s: %v", spec.Owner, spec.Repo, err)
	}

	return config
}

func downloadGitHubArchive(spec githubPackageSpec) []byte {
	return getBytes(githubAPIURL(spec, "zipball/"+spec.Ref))
}

func downloadReleaseAssetTo(release githubReleaseInfo, assetName string, dest string, spec githubPackageSpec) {
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			bytes := getBytes(asset.BrowserDownloadURL)
			err := os.MkdirAll(filepath.Dir(dest), 0755)
			if err != nil {
				LangError(ErrorRuntime, "failed to create plugin folder %s: %v", filepath.Dir(dest), err)
			}

			err = os.WriteFile(dest, bytes, 0644)
			if err != nil {
				LangError(ErrorRuntime, "failed to write plugin asset %s: %v", dest, err)
			}
			return
		}
	}

	LangError(ErrorRuntime, "latest GitHub release for %s/%s does not contain required asset %s", spec.Owner, spec.Repo, assetName)
}

func githubAPIURL(spec githubPackageSpec, path string) string {
	base := "https://api.github.com/repos/" + spec.Owner + "/" + spec.Repo
	if path == "" {
		return base
	}
	return base + "/" + path
}

func getJSON(url string, out any) {
	bytes := getBytes(url)
	err := json.Unmarshal(bytes, out)
	if err != nil {
		LangError(ErrorRuntime, "failed to parse GitHub response from %s: %v", url, err)
	}
}

func getBytes(url string) []byte {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		LangError(ErrorRuntime, "failed to create request for %s: %v", url, err)
	}

	req.Header.Set("User-Agent", "tiny-package-manager")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		LangError(ErrorRuntime, "failed to download %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		LangError(ErrorRuntime, "failed to download %s: %s", url, resp.Status)
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		LangError(ErrorRuntime, "failed to read response from %s: %v", url, err)
	}

	return bytes
}

func unpackGitHubArchive(data []byte, dest string) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		LangError(ErrorRuntime, "failed to read GitHub zip archive: %v", err)
	}

	rootPrefix := ""
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		if slash := strings.Index(name, "/"); slash >= 0 {
			rootPrefix = name[:slash+1]
			break
		}
	}

	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		if rootPrefix != "" {
			name = strings.TrimPrefix(name, rootPrefix)
		}
		if name == "" {
			continue
		}

		target := filepath.Join(dest, filepath.FromSlash(name))
		cleanDest := filepath.Clean(dest)
		cleanTarget := filepath.Clean(target)
		if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, cleanDest+string(filepath.Separator)) {
			LangError(ErrorRuntime, "unsafe path in GitHub zip archive: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			err := os.MkdirAll(target, 0755)
			if err != nil {
				LangError(ErrorRuntime, "failed to create folder from GitHub archive: %v", err)
			}
			continue
		}

		err := os.MkdirAll(filepath.Dir(target), 0755)
		if err != nil {
			LangError(ErrorRuntime, "failed to create folder from GitHub archive: %v", err)
		}

		in, err := file.Open()
		if err != nil {
			LangError(ErrorRuntime, "failed to open file from GitHub archive: %v", err)
		}

		out, err := os.Create(target)
		if err != nil {
			in.Close()
			LangError(ErrorRuntime, "failed to create file from GitHub archive: %v", err)
		}

		_, copyErr := io.Copy(out, in)
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copyErr != nil {
			LangError(ErrorRuntime, "failed to extract GitHub archive: %v", copyErr)
		}
		if closeInErr != nil {
			LangError(ErrorRuntime, "failed to close GitHub archive entry: %v", closeInErr)
		}
		if closeOutErr != nil {
			LangError(ErrorRuntime, "failed to close extracted file: %v", closeOutErr)
		}
	}
}

func copyDirectory(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		return copyFile(path, target)
	})
}

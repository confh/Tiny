package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "language.com/src/tinyerrors"
)

func runtimeFilenameForTarget(target string) (string, error) {
	switch target {
	case "windows-amd64":
		return "tiny_runtime_windows_amd64.exe", nil
	case "linux-amd64":
		return "tiny_runtime_linux_amd64", nil
	case "linux-arm64":
		return "tiny_runtime_linux_arm64", nil
	case "darwin-arm64":
		return "tiny_runtime_darwin_arm64", nil
	default:
		return "", fmt.Errorf("unsupported target: %s", target)
	}
}

func downloadRuntimeFile(url string, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	dir := filepath.Dir(destPath)
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		_ = os.Remove(tmpPath)
	}()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}

	err = f.Close()
	if err != nil {
		return err
	}

	if !strings.HasSuffix(destPath, ".exe") {
		err = os.Chmod(tmpPath, 0755)
		if err != nil {
			return err
		}
	}

	err = os.Rename(tmpPath, destPath)
	if err != nil {
		return err
	}

	return nil
}

func getRuntimeBytesForTarget(target string) []byte {
	filename, err := runtimeFilenameForTarget(target)
	if err != nil {
		LangError(ErrorRuntime, "%v", err)
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		LangError(ErrorRuntime, "failed to locate home directory: %v", err)
		return nil
	}

	runtimesDir := filepath.Join(homeDir, ".tiny", "runtimes")
	localPath := filepath.Join(runtimesDir, filename)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		fmt.Printf("Downloading runtime %s...\n", filename)
		url := fmt.Sprintf("https://github.com/confh/Tiny/releases/download/v%s/%s", TinyVersion, filename)
		err = downloadRuntimeFile(url, localPath)
		if err != nil {
			LangError(ErrorRuntime, "failed to download runtime for target %s: %v", target, err)
			return nil
		}
	}

	bytes, err := os.ReadFile(localPath)
	if err != nil {
		LangError(ErrorRuntime, "failed to read runtime file %s: %v", localPath, err)
		return nil
	}

	return bytes
}

func normalizePluginPathForTarget(path string, target string) string {
	ext := filepath.Ext(path)

	if ext != "" {
		return path
	}

	switch target {
	case "windows-amd64":
		return path + ".dll"

	case "linux-amd64", "linux-arm64":
		return path + ".so"

	case "darwin-arm64":
		return path + ".dylib"

	default:
		return path
	}
}

func pluginPathAppliesToTarget(path string, target string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return true
	}

	switch ext {
	case ".dll", ".so", ".dylib":
		return ext == pluginExtensionForTarget(target)
	default:
		return true
	}
}

func pluginExtensionForTarget(target string) string {
	switch target {
	case "windows-amd64":
		return ".dll"
	case "linux-amd64", "linux-arm64":
		return ".so"
	case "darwin-arm64":
		return ".dylib"
	default:
		return ""
	}
}

func normalizeTarget(target string) string {
	if target == "" {
		if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
			return "windows-amd64"
		} else if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			return "linux-amd64"
		} else if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
			return "linux-arm64"
		} else if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			return "darwin-arm64"
		}

		LangError(ErrorRuntime, "unsupported default target: %s-%s", runtime.GOOS, runtime.GOARCH)
	}

	return target
}

func packCommand(args []string) {
	entryFile := ""
	outFile := ""
	target := ""
	windowed := false

	start := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		entryFile = args[0]
		start = 1
	}

	for i := start; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				LangError(ErrorRuntime, "expected output path after -o")
			}

			outFile = args[i+1]
			i++

		case "--target":
			if i+1 >= len(args) {
				LangError(ErrorRuntime, "expected target after --target")
			}

			target = normalizeTarget(args[i+1])
			i++

		case "--windowed":
			windowed = true

		default:
			LangError(ErrorRuntime, "unknown pack argument: %s", args[i])
		}
	}

	if entryFile == "" {
		config, ok := loadTinyConfig()
		if !ok {
			LangError(ErrorRuntime, "usage: tiny pack <file.tiny> -o <output>")
		}

		entryFile = config.Entry

		name := config.Name
		if name == "" {
			name = "app"
		}

		if outFile == "" {
			outFile = filepath.Join(config.OutDir, name)
		}
		if target == "" {
			target = normalizeTarget(config.Target)
		}
	} else {
		if target == "" {
			target = normalizeTarget("")
		}
		if outFile == "" {
			outFile = defaultPackOutputName(entryFile, target)
		}
	}

	outFile = addExtensionForTarget(outFile, target)

	packToOutput(entryFile, outFile, target, windowed)

	fmt.Println("Packed:", outFile)
}

func addExtensionForTarget(path string, target string) string {
	if target == "windows-amd64" && filepath.Ext(path) == "" {
		return path + ".exe"
	}

	return path
}

var tinyPackMagic = []byte("TINYAPP1")

func writePackedExecutable(outFile string, runtimeBytes []byte, bytecodeBytes []byte) error {
	dir := filepath.Dir(outFile)

	if dir != "." && dir != "" {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	f, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(runtimeBytes)
	if err != nil {
		return err
	}

	_, err = f.Write(bytecodeBytes)
	if err != nil {
		return err
	}

	sizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBytes, uint64(len(bytecodeBytes)))

	_, err = f.Write(sizeBytes)
	if err != nil {
		return err
	}

	_, err = f.Write(tinyPackMagic)
	if err != nil {
		return err
	}

	return nil
}

func PatchPESubsystemToGUI(peBytes []byte) bool {
	if len(peBytes) < 64 {
		return false
	}

	if peBytes[0] != 'M' || peBytes[1] != 'Z' {
		return false
	}

	peOffset := binary.LittleEndian.Uint32(peBytes[0x3c:0x40])
	if int(peOffset+94) > len(peBytes) {
		return false
	}

	if peBytes[peOffset] != 'P' || peBytes[peOffset+1] != 'E' {
		return false
	}

	subsystem := binary.LittleEndian.Uint16(peBytes[peOffset+92 : peOffset+94])

	if subsystem == 3 {
		binary.LittleEndian.PutUint16(peBytes[peOffset+92:peOffset+94], 2)
		return true
	}

	return false
}

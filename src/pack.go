package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	. "language.com/src/tinyerrors"

	_ "embed"
)

//go:embed embedded/tiny_runtime_windows_amd64.exe
var embeddedRuntimeWindowsAMD64 []byte

//go:embed embedded/tiny_runtime_linux_amd64
var embeddedRuntimeLinuxAMD64 []byte

//go:embed embedded/tiny_runtime_darwin_arm64
var embeddedRuntimeDarwinARM64 []byte

func getEmbeddedRuntimeForTarget(target string) []byte {
	switch target {
	case "windows-amd64":
		return embeddedRuntimeWindowsAMD64
	case "linux-amd64":
		return embeddedRuntimeLinuxAMD64
	case "darwin-arm64":
		return embeddedRuntimeDarwinARM64
	default:
		LangError(ErrorRuntime, "unsupported target: %s", target)
		return nil
	}
}

func normalizePluginPathForTarget(path string, target string) string {
	ext := filepath.Ext(path)

	if ext != "" {
		return path
	}

	switch target {
	case "windows-amd64":
		return path + ".dll"

	case "linux-amd64":
		return path + ".so"

	case "darwin-arm64":
		return path + ".dylib"

	default:
		return path
	}
}

func normalizeTarget(target string) string {
	if target == "" {
		if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
			return "windows-amd64"
		} else if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			return "linux-amd64"
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
	target := normalizeTarget("")
	windowed := false

	for i := 1; i < len(args); i++ {
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

	if len(args) == 0 {
		config, ok := loadTinyConfig()
		if !ok {
			LangError(ErrorRuntime, "usage: tiny pack <file.tiny> -o <output>")
		}

		entryFile = config.Entry

		name := config.Name
		if name == "" {
			name = "app"
		}

		outFile = filepath.Join(config.OutDir, name)
		target = config.Target
	} else {
		entryFile = args[0]
		outFile = defaultPackOutputName(entryFile, target)
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

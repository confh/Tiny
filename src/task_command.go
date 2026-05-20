package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	. "language.com/src/tinyerrors"
)

func taskCommand(args []string) {
	if len(args) == 0 {
		config, ok := loadTinyConfig()
		if !ok {
			LangError(ErrorRuntime, "tiny.json not found")
		}

		if len(config.Scripts) == 0 {
			fmt.Println("No tasks defined in tiny.json")
			return
		}

		fmt.Println("Available tasks:")

		for name, command := range config.Scripts {
			fmt.Printf("  %s  ->  %s\n", name, command)
		}

		return
	}

	taskName := args[0]

	config, ok := loadTinyConfig()
	if !ok {
		LangError(ErrorRuntime, "tiny.json not found")
	}

	command, exists := config.Scripts[taskName]
	if !exists {
		LangError(ErrorName, "unknown task: %s", taskName)
	}

	extraArgs := args[1:]

	if len(extraArgs) > 0 {
		command += " " + strings.Join(extraArgs, " ")
	}

	fmt.Println("> " + command)

	runShellCommand(command)
}

func runShellCommand(command string) {
	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/C", command)
	} else {
		cmd = exec.Command("/bin/sh", "-c", command)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	if err != nil {
		LangError(ErrorRuntime, "task failed: %v", err)
	}
}

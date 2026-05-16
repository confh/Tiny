package vm

import (
	"strings"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdString(method string, args []Value) {
	switch method {
	case "upper":
		if len(args) != 1 {
			LangError(ErrorRuntime, "String.upper expects 1 argument")
		}

		text := asString(args[0])
		vm.push(strings.ToUpper(text))

	case "lower":
		if len(args) != 1 {
			LangError(ErrorRuntime, "String.lower expects 1 argument")
		}

		text := asString(args[0])
		vm.push(strings.ToLower(text))

	case "trim":
		if len(args) != 1 {
			LangError(ErrorRuntime, "String.trim expects 1 argument")
		}

		text := asString(args[0])
		vm.push(strings.TrimSpace(text))

	case "contains":
		if len(args) != 2 {
			LangError(ErrorRuntime, "String.contains expects 2 arguments")
		}

		text := asString(args[0])
		search := asString(args[1])

		vm.push(strings.Contains(text, search))

	case "replace":
		if len(args) != 3 {
			LangError(ErrorRuntime, "String.replace expects 3 arguments")
		}

		text := asString(args[0])
		oldText := asString(args[1])
		newText := asString(args[2])

		vm.push(strings.Replace(text, oldText, newText, 1))

	case "replaceAll":
		if len(args) != 3 {
			LangError(ErrorRuntime, "String.replaceAll expects 3 arguments")
		}

		text := asString(args[0])
		oldText := asString(args[1])
		newText := asString(args[2])

		vm.push(strings.ReplaceAll(text, oldText, newText))

	case "len":
		if len(args) != 1 {
			LangError(ErrorRuntime, "String.len expects 1 argument")
		}

		text := asString(args[0])
		vm.push(len(text))

	default:
		LangError(ErrorName, "unknown String function: %s", method)
	}
}

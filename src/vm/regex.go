package vm

import (
	"regexp"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdRegex(method string, args []Value) {
	switch method {
	case "matchString":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "regex.matchString expects 2 arguments")
		}

		str := asString(args[0], vm)
		regex := asString(args[1], vm)

		re := regexp.MustCompile(regex)

		vm.push(re.MatchString(str))

	case "findString":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "regex.findString expects 2 arguments")
		}

		str := asString(args[0], vm)
		regex := asString(args[1], vm)

		re := regexp.MustCompile(regex)

		vm.push(re.FindString(str))
	default:
		vm.runtimeError(ErrorName, "unknown regex function: %s", method)
	}
}

package vm

import (
	"strings"
)

type StdArg struct {
	Name     string
	Type     string
	Optional bool
	Variadic bool
}

type StdMethodInfo struct {
	Name        string
	Args        []StdArg
	Returns     string
	Description string
}

type StdModuleInfo struct {
	Name    string
	Methods map[string]StdMethodInfo
}

var StdMetadata = map[string]StdModuleInfo{}

func registerStdModule(info StdModuleInfo) {
	StdMetadata[info.Name] = info
}

func GetStdModuleInfo(name string) (StdModuleInfo, bool) {
	info, ok := StdMetadata[name]
	return info, ok
}

type NativeTypeInfo struct {
	Name    string
	Methods map[string]StdMethodInfo
}

var nativeTypeMetadata = map[string]NativeTypeInfo{}

func registerNativeType(info NativeTypeInfo) {
	nativeTypeMetadata[info.Name] = info
}

func GetNativeTypeInfo(name string) (NativeTypeInfo, bool) {
	if strings.HasPrefix(name, "array:") {
		name = "array"
	}
	info, ok := nativeTypeMetadata[name]
	return info, ok
}

type ArrayMethodMutator func(elementType string, info *StdMethodInfo)

var arrayMutators = map[string]ArrayMethodMutator{
	"get": func(el string, info *StdMethodInfo) {
		info.Returns = el
	},
	"pop": func(el string, info *StdMethodInfo) {
		info.Returns = el
	},
	"find": func(el string, info *StdMethodInfo) {
		info.Returns = el + " | null"
		if len(info.Args) > 0 {
			info.Args[0].Type = "function(" + el + ")"
		}
	},
	"push": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
		if len(info.Args) > 0 {
			info.Args[0].Type = el
		}
	},
	"set": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
		if len(info.Args) > 1 {
			info.Args[1].Type = el
		}
	},
	"reverse": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
	},
	"filter": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
		if len(info.Args) > 0 {
			info.Args[0].Type = "function(number, " + el + ")"
		}
	},
	"map": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
		if len(info.Args) > 0 {
			info.Args[0].Type = "function(number, " + el + ")"
		}
	},
	"forEach": func(el string, info *StdMethodInfo) {
		if len(info.Args) > 0 {
			info.Args[0].Type = "function(number, " + el + ")"
		}
	},
	"contains": func(el string, info *StdMethodInfo) {
		if len(info.Args) > 0 {
			info.Args[0].Type = el
		}
	},
	"reduce": func(el string, info *StdMethodInfo) {
		info.Returns = el
		if len(info.Args) > 0 {
			info.Args[0].Type = "function(number, " + el + ")"
		}
	},
	"sort": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
		if len(info.Args) > 0 {
			info.Args[0].Type = "function(any, " + el + ") | null"
		}
	},
	"slice": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
	},
	"flat": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
	},
	"flatMap": func(el string, info *StdMethodInfo) {
		info.Returns = "array:" + el
		if len(info.Args) > 0 {
			info.Args[0].Type = "function(number, " + el + ")"
		}
	},
	"findIndex": func(el string, info *StdMethodInfo) {
		if len(info.Args) > 0 {
			info.Args[0].Type = "function(" + el + ")"
		}
	},
}

func GetNativeMethodInfo(typeName string, method string) (StdMethodInfo, bool) {
	origTypeName := typeName
	isArray := strings.HasPrefix(typeName, "array:")

	if isArray {
		typeName = "array"
	}

	info, ok := GetNativeTypeInfo(typeName)
	if !ok {
		return StdMethodInfo{}, false
	}

	methodInfo, ok := info.Methods[method]
	if !ok {
		return StdMethodInfo{}, false
	}

	if isArray {
		cleanType := strings.Replace(origTypeName, "array:empty", "array:any", 1)
		elementType := strings.TrimPrefix(cleanType, "array:")

		if len(methodInfo.Args) > 0 {
			newArgs := make([]StdArg, len(methodInfo.Args))
			copy(newArgs, methodInfo.Args)
			methodInfo.Args = newArgs
		}

		if mutator, exists := arrayMutators[method]; exists {
			mutator(elementType, &methodInfo)
		}
	}

	return methodInfo, true
}

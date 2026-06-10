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
	if strings.HasPrefix(name, "array:") {
		name = "array"
	}
	info, ok := nativeTypeMetadata[name]
	return info, ok
}

func GetNativeMethodInfo(typeName string, method string) (StdMethodInfo, bool) {
	origTypeName := typeName
	if strings.HasPrefix(typeName, "array:") {
		typeName = "array"
	}
	info, ok := GetNativeTypeInfo(typeName)
	if !ok {
		return StdMethodInfo{}, false
	}

	methodInfo, ok := info.Methods[method]
	if ok && strings.HasPrefix(origTypeName, "array:") {
		elementType := strings.TrimPrefix(strings.Replace(origTypeName, "array:empty", "array:any", 1), "array:")
		if methodInfo.Returns == "any" && (method == "get" || method == "pop") {
			methodInfo.Returns = elementType
		} else if methodInfo.Returns == "array" && (method == "push" || method == "set" || method == "reverse" || method == "filter") {
			methodInfo.Returns = "array:" + elementType
		}

		if len(methodInfo.Args) > 0 {
			newArgs := make([]StdArg, len(methodInfo.Args))
			copy(newArgs, methodInfo.Args)
			methodInfo.Args = newArgs

			if method == "push" || method == "contains" {
				methodInfo.Args[0].Type = elementType
			} else if method == "set" && len(methodInfo.Args) > 1 {
				methodInfo.Args[1].Type = elementType
			}
		}
	}
	return methodInfo, ok
}

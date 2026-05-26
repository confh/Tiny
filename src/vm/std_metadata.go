package vm

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
	info, ok := nativeTypeMetadata[name]
	return info, ok
}

func GetNativeMethodInfo(typeName string, method string) (StdMethodInfo, bool) {
	info, ok := GetNativeTypeInfo(typeName)
	if !ok {
		return StdMethodInfo{}, false
	}

	methodInfo, ok := info.Methods[method]
	return methodInfo, ok
}

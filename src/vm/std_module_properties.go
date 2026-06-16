package vm

var stdModuleEnums = map[string]map[string]ObjectValue{}

func registerStdEnum(module string, enumName string, members ObjectValue) {
	if stdModuleEnums[module] == nil {
		stdModuleEnums[module] = map[string]ObjectValue{}
	}
	stdModuleEnums[module][enumName] = members
}

func getStdModuleProperty(module string, name string) (TinyValue, bool) {
	if enums, ok := stdModuleEnums[module]; ok {
		if members, ok := enums[name]; ok {
			return NewNative(members), true
		}
	}
	return NewNull(), false
}

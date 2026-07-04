package vm

import (
	"database/sql"
	_ "modernc.org/sqlite"
	. "language.com/src/tinyerrors"
)

var stdSqliteMetadata = StdModuleInfo{
	Name: "sqlite",
}

var stdSqliteMethods map[string]StdModuleFunc

func init() {
	stdSqliteMethods = map[string]StdModuleFunc{
		"open": stdSqliteOpen,
	}
	registerStdModule(stdSqliteMetadata)
}

func (vm *VM) callStdSqlite(method string, args []TinyValue) {
	fn, ok := stdSqliteMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown sqlite function: %s", method)
		return
	}
	fn(vm, args)
}

func stdSqliteOpen(vm *VM, args []TinyValue) {
	expectArgs(vm, "sqlite.open", args, 1)
	path := argString(vm, "sqlite.open", args, 0)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to open database: %v", err)
		return
	}

	vm.push(NewNative(&NativeSqliteValue{
		DB: db,
	}))
}

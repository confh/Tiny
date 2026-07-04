package vm

import (
	. "language.com/src/tinyerrors"
)

func (v *NativeSqliteValue) TinyTypeName() string {
	return "sqlite.Database"
}

var sqliteMethods map[string]NativeModuleFunc[*NativeSqliteValue]

func init() {
	sqliteMethods = map[string]NativeModuleFunc[*NativeSqliteValue]{
		"execute":      sqliteExecute,
		"query":        sqliteQuery,
		"queryRow":     sqliteQueryRow,
		"lastInsertId": sqliteLastInsertId,
		"close":        sqliteClose,
	}
}

func (vm *VM) callSqliteMethod(db *NativeSqliteValue, method string, args []TinyValue) {
	fn, ok := sqliteMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown sqlite.Database method: %s", method)
		return
	}
	fn(vm, db, args)
}

func parseQueryParams(vm *VM, args []TinyValue, index int) []any {
	if len(args) <= index || isNullish(args[index]) {
		return nil
	}
	arr := asArray(args[index], vm)
	goArgs := make([]any, len(arr.Elements))
	for i, v := range arr.Elements {
		goArgs[i] = v.Value
	}
	return goArgs
}

func normalizeDbValue(val any) TinyValue {
	if val == nil {
		return NewNull()
	}
	switch v := val.(type) {
	case int64:
		return NewInt(int(v))
	case int:
		return NewInt(v)
	case int32:
		return NewInt(int(v))
	case int16:
		return NewInt(int(v))
	case int8:
		return NewInt(int(v))
	case float64:
		return NewNative(v)
	case float32:
		return NewNative(float64(v))
	case bool:
		return NewNative(v)
	case string:
		return NewNative(v)
	case []byte:
		return NewNative(string(v))
	default:
		return NewNative(v)
	}
}

func sqliteExecute(vm *VM, db *NativeSqliteValue, args []TinyValue) {
	expectArgsRange(vm, "sqlite.Database.execute", args, 1, 2)
	query := argString(vm, "sqlite.Database.execute", args, 0)
	queryParams := parseQueryParams(vm, args, 1)

	res, err := db.DB.Exec(query, queryParams...)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "sqlite execution error: %v", err)
		return
	}

	rowsAffected, _ := res.RowsAffected()
	vm.push(NewInt(int(rowsAffected)))
}

func sqliteQuery(vm *VM, db *NativeSqliteValue, args []TinyValue) {
	expectArgsRange(vm, "sqlite.Database.query", args, 1, 2)
	query := argString(vm, "sqlite.Database.query", args, 0)
	queryParams := parseQueryParams(vm, args, 1)

	rows, err := db.DB.Query(query, queryParams...)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "sqlite query error: %v", err)
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []TinyValue

	for rows.Next() {
		columns := make([]any, len(cols))
		columnPointers := make([]any, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			vm.runtimeError(ErrorRuntime, "failed to scan row: %v", err)
			return
		}

		rowMap := ObjectValue{}
		for i, colName := range cols {
			val := columns[i]
			rowMap[colName] = normalizeDbValue(val)
		}
		results = append(results, NewNative(rowMap))
	}

	vm.push(NewArray(results))
}

func sqliteQueryRow(vm *VM, db *NativeSqliteValue, args []TinyValue) {
	sqliteQuery(vm, db, args)
	resArr := asArray(vm.pop(), vm)

	if len(resArr.Elements) > 0 {
		vm.push(resArr.Elements[0])
	} else {
		vm.push(NewNull())
	}
}

func sqliteLastInsertId(vm *VM, db *NativeSqliteValue, args []TinyValue) {
	var id int64
	err := db.DB.QueryRow("SELECT last_insert_rowid()").Scan(&id)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to get last insert id: %v", err)
		return
	}
	vm.push(NewInt(int(id)))
}

func sqliteClose(vm *VM, db *NativeSqliteValue, args []TinyValue) {
	dontExpectArgs(vm, "sqlite.Database.close", args)
	if !db.Closed {
		db.DB.Close()
		db.Closed = true
	}
	vm.push(NewNull())
}

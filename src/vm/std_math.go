package vm

import (
	"cmp"
	"math"
	"unsafe"

	"gonum.org/v1/gonum/mat"
	. "language.com/src/tinyerrors"
)

var stdMathMetadata = StdModuleInfo{
	Name: "math",
	Methods: map[string]StdMethodInfo{
		"toFloat": {
			Name: "toFloat",
			Args: []StdArg{
				{Name: "value", Type: "any", Optional: false},
			},
			Returns:     "float",
			Description: "Converts a value to a float.",
		},
		"toInt": {
			Name: "toInt",
			Args: []StdArg{
				{Name: "value", Type: "any", Optional: false},
			},
			Returns:     "int",
			Description: "Converts a value to an int.",
		},
		"abs": {
			Name: "abs",
			Args: []StdArg{
				{Name: "x", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Returns the absolute value of a number.",
		},
		"pow": {
			Name: "pow",
			Args: []StdArg{
				{Name: "base", Type: "float", Optional: false},
				{Name: "exp", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Raises base to the power of exp.",
		},
		"sqrt": {
			Name: "sqrt",
			Args: []StdArg{
				{Name: "x", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Returns the square root of a number.",
		},
		"ceil": {
			Name: "ceil",
			Args: []StdArg{
				{Name: "x", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Rounds a number upward to the nearest integer.",
		},
		"floor": {
			Name: "floor",
			Args: []StdArg{
				{Name: "x", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Rounds a number downward to the nearest integer.",
		},
		"round": {
			Name: "round",
			Args: []StdArg{
				{Name: "x", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Rounds a number to the nearest integer.",
		},
		"clamp": {
			Name: "clamp",
			Args: []StdArg{
				{Name: "value", Type: "float", Optional: false},
				{Name: "min", Type: "float", Optional: false},
				{Name: "max", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Clamps a float value between min and max.",
		},
		"sin": {
			Name: "sin",
			Args: []StdArg{
				{Name: "radians", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Returns the sine of an angle (in radians).",
		},
		"cos": {
			Name: "cos",
			Args: []StdArg{
				{Name: "radians", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Returns the cosine of an angle (in radians).",
		},
		"tan": {
			Name: "tan",
			Args: []StdArg{
				{Name: "radians", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Returns the tangent of an angle (in radians).",
		},
		"radToDeg": {
			Name: "radToDeg",
			Args: []StdArg{
				{Name: "radians", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Converts radians to degrees.",
		},
		"degToRad": {
			Name: "degToRad",
			Args: []StdArg{
				{Name: "degrees", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Converts degrees to radians.",
		},
		"atan2": {
			Name: "atan2",
			Args: []StdArg{
				{Name: "y", Type: "float", Optional: false},
				{Name: "x", Type: "float", Optional: false},
			},
			Returns:     "float",
			Description: "Returns atan2(y, x).",
		},
		"sum": {
			Name: "sum",
			Args: []StdArg{
				{Name: "buffer", Type: "buffer", Optional: false},
			},
			Returns:     "float",
			Description: "Returns the sum of all float64 values in a buffer.",
		},
		"matMul": {
			Name: "matMul",
			Args: []StdArg{
				{Name: "a", Type: "object", Optional: false},
				{Name: "b", Type: "object", Optional: false},
			},
			Returns:     "object",
			Description: "Performs matrix multiplication (returns a new matrix object).",
		},
		"matTranspose": {
			Name: "matTranspose",
			Args: []StdArg{
				{Name: "matrix", Type: "object", Optional: false},
			},
			Returns:     "object",
			Description: "Returns the transpose of a matrix object.",
		},
		"matScale": {
			Name: "matScale",
			Args: []StdArg{
				{Name: "matrix", Type: "object", Optional: false},
				{Name: "scalar", Type: "float", Optional: false},
			},
			Returns:     "object",
			Description: "Scales a matrix by a scalar (returns a new matrix object).",
		},
		"fib": {
			Name: "fib",
			Args: []StdArg{
				{Name: "n", Type: "int", Optional: false},
			},
			Returns:     "int",
			Description: "Returns the nth Fibonacci number (recursive implementation).",
		},
	},
}

var stdMathMethods map[string]StdModuleFunc

func init() {
	stdMathMethods = map[string]StdModuleFunc{
		"toFloat":      stdMathToFloat,
		"toInt":        stdMathToInt,
		"abs":          stdMathAbs,
		"pow":          stdMathPow,
		"sqrt":         stdMathSqrt,
		"ceil":         stdMathCeil,
		"floor":        stdMathFloor,
		"round":        stdMathRound,
		"clamp":        stdMathClamp,
		"sin":          stdMathSin,
		"cos":          stdMathCos,
		"tan":          stdMathTan,
		"radToDeg":     stdMathRadToDeg,
		"degToRad":     stdMathDegToRad,
		"atan2":        stdMathAtan2,
		"sum":          stdMathSum,
		"matMul":       stdMathMatMul,
		"matTranspose": stdMathMatTranspose,
		"matScale":     stdMathMatScale,
		"fib":          stdMathFib,
	}
	registerStdModule(stdMathMetadata)
}

func (vm *VM) callStdMath(method string, args []Value) {
	fn, ok := stdMathMethods[method]
	if !ok {
		vm.push(NewUndefined())
		return
	}
	fn(vm, args)
}

func Clamp[T cmp.Ordered](val, min, max T) T {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func RadToDeg(rad float64) float64 {
	return rad * (180 / math.Pi)
}

func DegToRad(deg float64) float64 {
	return deg * (math.Pi / 180)
}

func nativeFib(n int) int {
	if n < 2 {
		return n
	}

	a, b := 0, 1
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

func getMatrixFields(v ObjectValue, matName string, vm *VM) (int, int, []float64) {
	rows := v["rows"]
	if !rows.IsInt {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'rows' field", matName)
		vm.push(NewUndefined())
		return 0, 0, nil
	}
	cols := v["cols"]
	if !cols.IsInt {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'cols' field", matName)
		vm.push(NewUndefined())
		return 0, 0, nil
	}
	rawData, ok := v["data"].Value.(*BufferValue)
	if !ok {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'data' field", matName)
		vm.push(NewUndefined())
		return 0, 0, nil
	}

	if len(rawData.Bytes) == 0 {
		return rows.AsInt, cols.AsInt, nil
	}

	data := unsafe.Slice((*float64)(unsafe.Pointer(&rawData.Bytes[0])), len(rawData.Bytes)/8)

	return rows.AsInt, cols.AsInt, data
}

func float64SliceToBytes(data []float64) []byte {
	if len(data) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*8)
}

func stdMathToFloat(vm *VM, args []Value) {
	expectArgs(vm, "math.toFloat", args, 1)
	val := asFloat(args[0], vm)
	vm.push(NewNative(val))
}

func stdMathToInt(vm *VM, args []Value) {
	expectArgs(vm, "math.toInt", args, 1)
	val := int(asFloat(args[0], vm))
	vm.push(NewInt(val))
}

func stdMathAbs(vm *VM, args []Value) {
	expectArgs(vm, "math.abs", args, 1)
	value := asFloat64(args[0])
	vm.push(NewNative(math.Abs(value)))
}

func stdMathPow(vm *VM, args []Value) {
	expectArgs(vm, "math.pow", args, 2)
	base := asFloat64(args[0])
	exp := asFloat64(args[1])
	vm.push(NewNative(math.Pow(base, exp)))
}

func stdMathSqrt(vm *VM, args []Value) {
	expectArgs(vm, "math.sqrt", args, 1)
	x := asFloat64(args[0])
	vm.push(NewNative(math.Sqrt(x)))
}

func stdMathCeil(vm *VM, args []Value) {
	expectArgs(vm, "math.ceil", args, 1)
	x := asFloat64(args[0])
	vm.push(NewNative(math.Ceil(x)))
}

func stdMathFloor(vm *VM, args []Value) {
	expectArgs(vm, "math.floor", args, 1)
	x := asFloat64(args[0])
	vm.push(NewNative(math.Floor(x)))
}

func stdMathRound(vm *VM, args []Value) {
	expectArgs(vm, "math.round", args, 1)
	x := asFloat64(args[0])
	vm.push(NewNative(math.Round(x)))
}

func stdMathClamp(vm *VM, args []Value) {
	expectArgs(vm, "math.clamp", args, 3)
	value := asFloat64(args[0])
	min := asFloat64(args[1])
	max := asFloat64(args[2])
	vm.push(NewNative(Clamp(value, min, max)))
}

func stdMathSin(vm *VM, args []Value) {
	expectArgs(vm, "math.sin", args, 1)
	rad := asFloat64(args[0])
	vm.push(NewNative(math.Sin(rad)))
}

func stdMathCos(vm *VM, args []Value) {
	expectArgs(vm, "math.cos", args, 1)
	rad := asFloat64(args[0])
	vm.push(NewNative(math.Cos(rad)))
}

func stdMathTan(vm *VM, args []Value) {
	expectArgs(vm, "math.tan", args, 1)
	rad := asFloat64(args[0])
	vm.push(NewNative(math.Tan(rad)))
}

func stdMathRadToDeg(vm *VM, args []Value) {
	expectArgs(vm, "math.radToDeg", args, 1)
	rad := asFloat64(args[0])
	vm.push(NewNative(RadToDeg(rad)))
}

func stdMathDegToRad(vm *VM, args []Value) {
	expectArgs(vm, "math.degToRad", args, 1)
	deg := asFloat64(args[0])
	vm.push(NewNative(DegToRad(deg)))
}

func stdMathAtan2(vm *VM, args []Value) {
	expectArgs(vm, "math.atan2", args, 2)
	y := asFloat64(args[0])
	x := asFloat64(args[1])
	vm.push(NewNative(math.Atan2(y, x)))
}

func stdMathSum(vm *VM, args []Value) {
	expectArgs(vm, "math.sum", args, 1)
	buf := asBuffer(args[0], vm)
	if len(buf.Bytes) == 0 {
		vm.push(NewNative(0.0))
		return
	}
	floats := unsafe.Slice((*float64)(unsafe.Pointer(&buf.Bytes[0])), len(buf.Bytes)/8)
	var total float64
	for _, val := range floats {
		total += val
	}
	vm.push(NewNative(total))
}

func stdMathMatMul(vm *VM, args []Value) {
	expectArgs(vm, "math.matMul", args, 2)
	aValue := asObject(args[0], vm)
	bValue := asObject(args[1], vm)

	aRows, aCols, aData := getMatrixFields(aValue, "first", vm)
	bRows, bCols, bData := getMatrixFields(bValue, "second", vm)

	if aCols != bRows {
		vm.push(NewUndefined())
		return
	}

	a := mat.NewDense(aRows, aCols, aData)
	b := mat.NewDense(bRows, bCols, bData)
	var res mat.Dense
	res.Mul(a, b)
	r, c := res.Dims()
	resultData := res.RawMatrix().Data
	vm.push(NewNative(ObjectValue{
		"rows": NewInt(r),
		"cols": NewInt(c),
		"data": NewNative(&BufferValue{
			Bytes: float64SliceToBytes(resultData),
		}),
	}))
}

func stdMathMatTranspose(vm *VM, args []Value) {
	expectArgs(vm, "math.matTranspose", args, 1)
	value := asObject(args[0], vm)
	rows, cols, data := getMatrixFields(value, "first", vm)
	m := mat.NewDense(rows, cols, data)
	transposed := m.T()
	var res mat.Dense
	res.CloneFrom(transposed)
	r, c := res.Dims()
	resultData := res.RawMatrix().Data
	vm.push(NewNative(ObjectValue{
		"rows": NewInt(r),
		"cols": NewInt(c),
		"data": NewNative(&BufferValue{
			Bytes: float64SliceToBytes(resultData),
		}),
	}))
}

func stdMathMatScale(vm *VM, args []Value) {
	expectArgs(vm, "math.matScale", args, 2)
	value := asObject(args[0], vm)
	scalar := asFloat64(args[1])
	rows, cols, data := getMatrixFields(value, "first", vm)
	m := mat.NewDense(rows, cols, data)
	var res mat.Dense
	res.Scale(scalar, m)
	r, c := res.Dims()
	resultData := res.RawMatrix().Data
	vm.push(NewNative(ObjectValue{
		"rows": NewInt(r),
		"cols": NewInt(c),
		"data": NewNative(&BufferValue{
			Bytes: float64SliceToBytes(resultData),
		}),
	}))
}

func stdMathFib(vm *VM, args []Value) {
	expectArgs(vm, "math.fib", args, 1)
	n := argInt(vm, "math.fib", args, 0)
	vm.push(NewInt(nativeFib(n)))
}

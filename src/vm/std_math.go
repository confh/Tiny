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
	}
	registerStdModule(stdMathMetadata)
}

func (vm *VM) callStdMath(method string, args []TinyValue) {
	fn, ok := stdMathMethods[method]
	if !ok {
		vm.push(NewNull())
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

func getMatrixFields(v ObjectValue, matName string, vm *VM) (int, int, []float64) {
	rows := v["rows"]
	if !rows.IsInt {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'rows' field", matName)
		vm.push(NewNull())
		return 0, 0, nil
	}
	cols := v["cols"]
	if !cols.IsInt {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'cols' field", matName)
		vm.push(NewNull())
		return 0, 0, nil
	}
	rawData, ok := v["data"].Value.(*BufferValue)
	if !ok {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'data' field", matName)
		vm.push(NewNull())
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

func stdMathToFloat(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.toFloat", args, 1)
	val := asFloat(args[0], vm)
	vm.push(NewNative(val))
}

func stdMathToInt(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.toInt", args, 1)
	val := int(asFloat(args[0], vm))
	vm.push(NewInt(val))
}

func stdMathAbs(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.abs", args, 1)
	value := vm.asFloat64(args[0])
	vm.push(NewNative(math.Abs(value)))
}

func stdMathPow(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.pow", args, 2)
	base := vm.asFloat64(args[0])
	exp := vm.asFloat64(args[1])
	vm.push(NewNative(math.Pow(base, exp)))
}

func stdMathSqrt(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.sqrt", args, 1)
	x := vm.asFloat64(args[0])
	vm.push(NewNative(math.Sqrt(x)))
}

func stdMathCeil(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.ceil", args, 1)
	x := vm.asFloat64(args[0])
	vm.push(NewNative(math.Ceil(x)))
}

func stdMathFloor(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.floor", args, 1)
	x := vm.asFloat64(args[0])
	vm.push(NewNative(math.Floor(x)))
}

func stdMathRound(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.round", args, 1)
	x := vm.asFloat64(args[0])
	vm.push(NewNative(math.Round(x)))
}

func stdMathClamp(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.clamp", args, 3)
	value := vm.asFloat64(args[0])
	min := vm.asFloat64(args[1])
	max := vm.asFloat64(args[2])
	vm.push(NewNative(Clamp(value, min, max)))
}

func stdMathSin(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.sin", args, 1)
	rad := vm.asFloat64(args[0])
	vm.push(NewNative(math.Sin(rad)))
}

func stdMathCos(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.cos", args, 1)
	rad := vm.asFloat64(args[0])
	vm.push(NewNative(math.Cos(rad)))
}

func stdMathTan(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.tan", args, 1)
	rad := vm.asFloat64(args[0])
	vm.push(NewNative(math.Tan(rad)))
}

func stdMathRadToDeg(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.radToDeg", args, 1)
	rad := vm.asFloat64(args[0])
	vm.push(NewNative(RadToDeg(rad)))
}

func stdMathDegToRad(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.degToRad", args, 1)
	deg := vm.asFloat64(args[0])
	vm.push(NewNative(DegToRad(deg)))
}

func stdMathAtan2(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.atan2", args, 2)
	y := vm.asFloat64(args[0])
	x := vm.asFloat64(args[1])
	vm.push(NewNative(math.Atan2(y, x)))
}

func stdMathSum(vm *VM, args []TinyValue) {
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

func stdMathMatMul(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.matMul", args, 2)
	aValue := asObject(args[0], vm)
	bValue := asObject(args[1], vm)

	aRows, aCols, aData := getMatrixFields(aValue, "first", vm)
	bRows, bCols, bData := getMatrixFields(bValue, "second", vm)

	if aCols != bRows {
		vm.push(NewNull())
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

func stdMathMatTranspose(vm *VM, args []TinyValue) {
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

func stdMathMatScale(vm *VM, args []TinyValue) {
	expectArgs(vm, "math.matScale", args, 2)
	value := asObject(args[0], vm)
	scalar := vm.asFloat64(args[1])
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

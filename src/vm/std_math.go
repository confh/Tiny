package vm

import (
	"unsafe"

	"gonum.org/v1/gonum/blas/blas64"
	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/netlib/blas/netlib"
	_ "gonum.org/v1/netlib/blas/netlib"
	. "language.com/src/tinyerrors"
)

func getMatrixFields(v ObjectValue, matName string, vm *VM) (int, int, []float64) {
	rows, ok := v["rows"].(int)
	if !ok {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'rows' field", matName)
	}
	cols, ok := v["cols"].(int)
	if !ok {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'cols' field", matName)
	}
	rawData, ok := v["data"].(*BufferValue)
	if !ok {
		vm.runtimeError(ErrorType, "%s matrix missing or invalid 'data' field", matName)
	}

	if len(rawData.Bytes) == 0 {
		return rows, cols, nil
	}

	// Reinterpret byte slice to float64 slice safely
	data := unsafe.Slice((*float64)(unsafe.Pointer(&rawData.Bytes[0])), len(rawData.Bytes)/8)

	return rows, cols, data
}

// Helper to safely turn a float64 slice into a byte slice without crashing on empty data
func float64SliceToBytes(data []float64) []byte {
	if len(data) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*8)
}

func (vm *VM) callStdMath(method string, args []Value) {
	blas64.Use(netlib.Implementation{})
	switch method {
	case "toFloat":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "math.toFloat expects 1 argument")
		}
		vm.push(asFloat(args[0]))

	case "toInt":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "math.toInt expects 1 argument")
		}
		vm.push(int(asFloat(args[0])))

	case "matMul":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "math.matMul expects 2 arguments")
		}

		aValue := asObject(args[0], vm)
		bValue := asObject(args[1], vm)

		aRows, aCols, aData := getMatrixFields(aValue, "first", vm)
		bRows, bCols, bData := getMatrixFields(bValue, "second", vm)

		// Size check to prevent Gonum from panicking
		if aCols != bRows {
			vm.runtimeError(ErrorRuntime, "matrix multiply size mismatch: %dx%d and %dx%d", aRows, aCols, bRows, bCols)
		}

		a := mat.NewDense(aRows, aCols, aData)
		b := mat.NewDense(bRows, bCols, bData)

		var res mat.Dense
		res.Mul(a, b) // ⚡️ Gonum optimizes this internally across CPU threads!

		r, c := res.Dims()
		resultData := res.RawMatrix().Data

		vm.push(ObjectValue{
			"rows": r,
			"cols": c,
			"data": &BufferValue{
				Bytes: float64SliceToBytes(resultData),
			},
		})

	case "matTranspose":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "math.matTranspose expects 1 argument")
		}

		value := asObject(args[0], vm)
		rows, cols, data := getMatrixFields(value, "first", vm)

		m := mat.NewDense(rows, cols, data)
		transposed := m.T()

		var res mat.Dense
		res.CloneFrom(transposed)

		r, c := res.Dims()
		resultData := res.RawMatrix().Data // FIXED: Get the actual transposed data layout

		vm.push(ObjectValue{
			"rows": r,
			"cols": c,
			"data": &BufferValue{
				Bytes: float64SliceToBytes(resultData),
			},
		})

	case "matScale":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "math.matScale expects 2 arguments")
		}

		value := asObject(args[0], vm)
		scalar := asFloat64(args[1]) // Make sure your type helper matches your VM needs

		rows, cols, data := getMatrixFields(value, "first", vm)

		m := mat.NewDense(rows, cols, data)

		var res mat.Dense
		res.Scale(scalar, m)

		r, c := res.Dims()
		resultData := res.RawMatrix().Data // FIXED: Get scaled data

		// FIXED: Returning full ObjectValue matrix instead of raw buffer
		vm.push(ObjectValue{
			"rows": r,
			"cols": c,
			"data": &BufferValue{
				Bytes: float64SliceToBytes(resultData),
			},
		})

	default:
		vm.runtimeError(ErrorName, "unknown math function: %s", method)
	}
}

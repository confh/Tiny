package vm

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"

	. "language.com/src/tinyerrors"
)

var stdCryptoMetadata = StdModuleInfo{
	Name: "crypto",
}

var stdCryptoMethods map[string]StdModuleFunc

func init() {
	stdCryptoMethods = map[string]StdModuleFunc{
		"hash": cryptohash,
		"uuid": cryptoUUID,
	}
	registerStdModule(stdCryptoMetadata)
	registerStdEnum("crypto", "Algorithms", ObjectValue{
		"SHA256": NewNative("sha256"),
		"SHA512": NewNative("sha512"),
		"SHA1":   NewNative("sha1"),
		"MD5":    NewNative("md5"),
	})
}

func (vm *VM) callStdCrypto(method string, args []TinyValue) {
	fn, ok := stdCryptoMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown crypto function: %s", method)
		return
	}

	fn(vm, args)
}

func cryptohash(vm *VM, args []TinyValue) {
	expectArgs(vm, "crypto.hash", args, 2)

	method := argString(vm, "crypto.hash", args, 0)
	data := args[1]

	var bytesToHash []byte

	switch v := data.Value.(type) {
	case string:
		bytesToHash = []byte(v)

	case BufferValue:
		bytesToHash = v.Bytes

	default:
		vm.runtimeError(ErrorRuntime, "crypto.hash expects string or buffer as second argument, got %s", TypeName(data))
	}

	switch method {
	case "sha256":
		hashArray := sha256.Sum256(bytesToHash)

		vm.push(NewNative(&BufferValue{Bytes: hashArray[:]}))

	case "sha512":
		hashArray := sha512.Sum512(bytesToHash)

		vm.push(NewNative(&BufferValue{Bytes: hashArray[:]}))

	case "sha1":
		hashArray := sha1.Sum(bytesToHash)

		vm.push(NewNative(&BufferValue{Bytes: hashArray[:]}))

	case "md5":
		hashArray := md5.Sum(bytesToHash)

		vm.push(NewNative(&BufferValue{Bytes: hashArray[:]}))

	default:
		vm.runtimeError(ErrorRuntime, "crypto.hash expected a valid method, got %s", method)
	}
}

func cryptoUUID(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "crypto.uuid", args)
	b := make([]byte, 16)
	rand.Read(b)

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	vm.push(NewNative(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])))
}

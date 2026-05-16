package tinyplugin

/*
#include <stdlib.h>
*/

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

type Request struct {
	Method string `json:"method"`
	Args   []any  `json:"args"`
}

type Handler func(args Args) (any, error)

var handlers = map[string]Handler{}

func Register(name string, handler Handler) {
	handlers[name] = handler
}

func HandleCall(method string, argsJSON string) string {
	var req Request

	if err := json.Unmarshal([]byte(argsJSON), &req); err != nil {
		return ErrorResult("PluginError", err.Error())
	}

	handler, exists := handlers[method]
	if !exists {
		return ErrorResult("PluginError", "unknown method: "+method)
	}

	result, err := safeCall(handler, Args{values: req.Args})
	if err != nil {
		var pluginErr *PluginError
		if errors.As(err, &pluginErr) {
			return ErrorResult(pluginErr.Kind, pluginErr.Message)
		}

		return ErrorResult("PluginError", err.Error())
	}

	return JSONResult(result)
}

func JSONResult(value any) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		return ErrorResult("PluginError", "failed to encode response: "+err.Error())
	}

	return string(bytes)
}

func ErrorResult(kind string, message string) string {
	bytes, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"kind":    kind,
			"message": message,
		},
	})

	return string(bytes)
}

type PluginError struct {
	Kind    string
	Message string
}

func (e *PluginError) Error() string {
	return e.Kind + ": " + e.Message
}

func Error(kind string, message string) error {
	return &PluginError{
		Kind:    kind,
		Message: message,
	}
}

type Args struct {
	values []any
}

func (a Args) Len() int {
	return len(a.values)
}

func (a Args) Raw(index int) any {
	if index < 0 || index >= len(a.values) {
		panic(fmt.Sprintf("argument %d out of range", index))
	}

	return a.values[index]
}

func (a Args) String(index int) string {
	value := a.Raw(index)

	text, ok := value.(string)
	if !ok {
		panic(fmt.Sprintf("argument %d must be string", index))
	}

	return text
}

func (a Args) Float(index int) float64 {
	value := a.Raw(index)

	number, ok := value.(float64)
	if !ok {
		panic(fmt.Sprintf("argument %d must be number", index))
	}

	return number
}

func (a Args) Int(index int) int {
	return int(a.Float(index))
}

func (a Args) Bool(index int) bool {
	value := a.Raw(index)

	boolean, ok := value.(bool)
	if !ok {
		panic(fmt.Sprintf("argument %d must be bool", index))
	}

	return boolean
}

func (a Args) Object(index int) map[string]any {
	value := a.Raw(index)

	object, ok := value.(map[string]any)
	if !ok {
		panic(fmt.Sprintf("argument %d must be object", index))
	}

	return object
}

func (a Args) Array(index int) []any {
	value := a.Raw(index)

	array, ok := value.([]any)
	if !ok {
		panic(fmt.Sprintf("argument %d must be array", index))
	}

	return array
}

func safeCall(handler Handler, args Args) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()

	return handler(args)
}

type HandleStore[T any] struct {
	mu     sync.Mutex
	prefix string
	nextID int
	items  map[string]T
}

func NewHandleStore[T any](prefix string) *HandleStore[T] {
	return &HandleStore[T]{
		prefix: prefix,
		nextID: 1,
		items:  map[string]T{},
	}
}

func (s *HandleStore[T]) Add(value T) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	handle := s.prefix + "_" + strconv.Itoa(s.nextID)
	s.nextID++

	s.items[handle] = value

	return handle
}

func (s *HandleStore[T]) Get(handle string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, exists := s.items[handle]
	return value, exists
}

func (s *HandleStore[T]) MustGet(handle string) T {
	value, exists := s.Get(handle)
	if !exists {
		panic("unknown handle: " + handle)
	}

	return value
}

func (s *HandleStore[T]) Delete(handle string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.items, handle)
}

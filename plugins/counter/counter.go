package main

import "language.com/src/tinyplugin"

type Counter struct {
	Name  string
	Value int
}

var counters = tinyplugin.NewHandleStore[*Counter]("counter")

func init() {
	tinyplugin.Register("new", func(args tinyplugin.Args) (any, error) {
		if args.Len() != 1 {
			return nil, tinyplugin.Error("PluginError", "new expects 1 argument")
		}

		name := args.String(0)

		handle := counters.Add(&Counter{
			Name:  name,
			Value: 0,
		})

		return map[string]any{
			"handle": handle,
		}, nil
	})

	tinyplugin.Register("inc", func(args tinyplugin.Args) (any, error) {
		if args.Len() != 1 {
			return nil, tinyplugin.Error("PluginError", "inc expects 1 argument")
		}

		handle := args.String(0)
		counter := counters.MustGet(handle)

		counter.Value++

		return map[string]any{
			"value": counter.Value,
		}, nil
	})

	tinyplugin.Register("add", func(args tinyplugin.Args) (any, error) {
		if args.Len() != 2 {
			return nil, tinyplugin.Error("PluginError", "add expects 2 arguments")
		}

		handle := args.String(0)
		amount := args.Int(1)

		counter := counters.MustGet(handle)
		counter.Value += amount

		return map[string]any{
			"value": counter.Value,
		}, nil
	})

	tinyplugin.Register("get", func(args tinyplugin.Args) (any, error) {
		if args.Len() != 1 {
			return nil, tinyplugin.Error("PluginError", "get expects 1 argument")
		}

		handle := args.String(0)
		counter := counters.MustGet(handle)

		return map[string]any{
			"name":  counter.Name,
			"value": counter.Value,
		}, nil
	})

	tinyplugin.Register("reset", func(args tinyplugin.Args) (any, error) {
		if args.Len() != 1 {
			return nil, tinyplugin.Error("PluginError", "reset expects 1 argument")
		}

		handle := args.String(0)
		counter := counters.MustGet(handle)
		counter.Value = 0

		return map[string]any{
			"value": counter.Value,
		}, nil
	})
}

package main

import (
	"runtime"
	"strconv"

	g "github.com/AllenDang/giu"
	"language.com/src/tinyplugin"
)

func init() {
	tinyplugin.Register("show", func(args tinyplugin.Args) (any, error) {
		if args.Len() != 1 {
			return nil, tinyplugin.Error("PluginError", "imgui.show expects 1 object argument")
		}

		config := args.Object(0)

		title := stringOrDefault(config, "title", "Tiny ImGui App")
		text := stringOrDefault(config, "text", "Hello from Tiny")
		width := intOrDefault(config, "width", 600)
		height := intOrDefault(config, "height", 400)

		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		clicks := 0

		wnd := g.NewMasterWindow(title, width, height, 0)

		wnd.Run(func() {
			g.SingleWindow().Layout(
				g.Label("Tiny + Dear ImGui"),
				g.Separator(),
				g.Label(text),
				g.Button("Click me").OnClick(func() {
					clicks++
				}),
				g.Label("Clicks: "+strconv.Itoa(clicks)),
				g.Separator(),
				g.Label("Close the window to return to Tiny."),
			)
		})

		return map[string]any{
			"closed": true,
			"clicks": clicks,
		}, nil
	})
}

func stringOrDefault(obj map[string]any, key string, fallback string) string {
	value, exists := obj[key]
	if !exists {
		return fallback
	}

	text, ok := value.(string)
	if !ok {
		return fallback
	}

	return text
}

func intOrDefault(obj map[string]any, key string, fallback int) int {
	value, exists := obj[key]
	if !exists {
		return fallback
	}

	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return fallback
	}
}

package main

import (
	"fmt"

	"language.com/compiler/tinyplugin"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func init() {
	tinyplugin.Register("show", func(args tinyplugin.Args) (any, error) {
		if args.Len() != 1 {
			return nil, tinyplugin.Error("PluginError", "show expects 1 object argument")
		}

		config := args.Object(0)

		title := stringOrDefault(config, "title", "Tiny Fyne App")
		text := stringOrDefault(config, "text", "Hello from Tiny")
		width := intOrDefault(config, "width", 500)
		height := intOrDefault(config, "height", 300)

		a := app.New()
		w := a.NewWindow(title)

		label := widget.NewLabel(text)
		label.Wrapping = fyne.TextWrapWord

		content := container.NewVBox(
			widget.NewLabel("Tiny + Fyne GUI"),
			label,
			widget.NewButton("Close", func() {
				w.Close()
			}),
		)

		w.SetContent(content)
		w.Resize(fyne.NewSize(float32(width), float32(height)))
		w.ShowAndRun()

		return map[string]any{
			"closed": true,
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
		fmt.Println("invalid int field:", key)
		return fallback
	}
}

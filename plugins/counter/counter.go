package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"

	"language.com/src/tinyplugin"
)

func init() {
	tinyplugin.Register("hash", func(args tinyplugin.Args) (any, error) {
		// 1. Validation: Expecting 1 configuration object argument
		if args.Len() != 1 {
			return nil, tinyplugin.Error("PluginError", "crypto.hash expects 1 object argument")
		}

		config := args.Object(0)

		// 2. Extract values from the Tiny object passed via JSON
		text, hasText := config["text"].(string)
		method, hasMethod := config["method"].(string)

		if !hasText || !hasMethod {
			return nil, tinyplugin.Error("ValidationError", "Missing 'text' or 'method' fields")
		}

		var hashedResult string

		// 3. Process the data using native Go speed
		switch method {
		case "md5":
			hash := md5.Sum([]byte(text))
			hashedResult = hex.EncodeToString(hash[:])
		case "sha256":
			hash := sha256.Sum256([]byte(text))
			hashedResult = hex.EncodeToString(hash[:])
		default:
			return nil, tinyplugin.Error("UnsupportedError", "Unknown method: "+method)
		}

		// 4. Return a clean JSON map back to Tiny
		return map[string]any{
			"success": true,
			"method":  method,
			"input":   text,
			"result":  hashedResult,
		}, nil
	})
}

package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"

	"language.com/src/tinyplugin"
)

func init() {
	tinyplugin.Register("hash", func(args tinyplugin.Args) (any, error) {
		if args.Len() != 1 {
			return nil, tinyplugin.Error("PluginError", "crypto.hash expects 1 object argument")
		}

		config := args.Object(0)

		text, hasText := config["text"].(string)
		method, hasMethod := config["method"].(string)

		if !hasText || !hasMethod {
			return nil, tinyplugin.Error("ValidationError", "Missing 'text' or 'method' fields")
		}

		var hashedResult string

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

		return map[string]any{
			"success": true,
			"method":  method,
			"input":   text,
			"result":  hashedResult,
		}, nil
	})
}

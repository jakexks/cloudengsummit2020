package main

import "encoding/base64"

func toBase64(in string) string {
	return base64.StdEncoding.EncodeToString([]byte(in))
}

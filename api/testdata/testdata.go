// Package testdata holds the wire-contract fixtures: the exact JSON bytes
// the daemon sends for one message family each, written by the wire-contract
// test and read back by the mock server.
package testdata

import (
	"embed"
	"encoding/json"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// FS holds every fixture file.
//
//go:embed *.json
var FS embed.FS

// Decode reads the fixture called name, without its extension, into msg. A
// message carrying an enum with a custom wire spelling unmarshals itself;
// the rest need protojson, because encoding/json cannot read the wire form
// of a proto message.
func Decode[T proto.Message](name string, msg T) (T, error) {
	data, err := FS.ReadFile(name + ".json")
	if err != nil {
		return msg, err
	}
	if u, ok := any(msg).(json.Unmarshaler); ok {
		return msg, u.UnmarshalJSON(data)
	}
	return msg, protojson.Unmarshal(data, msg)
}

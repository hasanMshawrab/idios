package main

import (
	"strings"
	"testing"
)

// presentation.md, configuration: a different port is the only expected
// change to api_listen, and it must stay loopback-bound.
func TestLoadConfigListenOverride(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		listen  string
		want    string
		wantErr string
	}{
		{name: "empty keeps the default", listen: "", want: "127.0.0.1:7770"},
		{name: "loopback override lands", listen: "127.0.0.1:7771", want: "127.0.0.1:7771"},
		{name: "non-loopback is rejected", listen: "0.0.0.0:7771", wantErr: "must bind 127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig("", dir, "", tt.listen)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("loadConfig error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.APIListen != tt.want {
				t.Fatalf("APIListen = %q, want %q", cfg.APIListen, tt.want)
			}
		})
	}
}

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExampleComposition(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     config
		wantErr bool
		want    string
	}{
		{name: "offline success", cfg: config{Origin: "https://example.test"}, want: "health=204 public-pricing=true\ntelemetry stopped\n"},
		{name: "invalid origin rolls back", cfg: config{Origin: "invalid"}, wantErr: true, want: "telemetry stopped\n"},
		{name: "enabled needs dependencies", cfg: config{Origin: "https://example.test", EnableIndexNow: true}, wantErr: true, want: "telemetry stopped\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(context.Background(), &output, tc.cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("run error = %v", err)
			}
			if output.String() != tc.want {
				t.Fatalf("output = %q", output.String())
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("unexpected sensitive error")
			}
		})
	}
}

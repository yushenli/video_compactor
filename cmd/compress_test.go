package cmd

import (
	"testing"
)

func TestNewCompressCmdRegistersFlags(t *testing.T) {
	cmd := newCompressCmd()

	tests := []struct {
		flagName    string
		wantDefault string
	}{
		{"vaapi-device", ""},
		{"codec", ""},
		{"dry-run", "false"},
	}

	for _, tc := range tests {
		t.Run(tc.flagName, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tc.flagName)
			if flag == nil {
				t.Fatalf("expected --%s flag to be registered", tc.flagName)
			}
			if flag.DefValue != tc.wantDefault {
				t.Errorf("--%s default = %q, want %q", tc.flagName, flag.DefValue, tc.wantDefault)
			}
		})
	}
}

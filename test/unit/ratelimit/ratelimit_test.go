package ratelimit_test

import (
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/ratelimit"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ratelimit.Config
		wantErr bool
	}{
		{name: "valid", cfg: ratelimit.Config{Tokens: 5, Interval: time.Second}, wantErr: false},
		{name: "zero tokens", cfg: ratelimit.Config{Tokens: 0, Interval: time.Second}, wantErr: true},
		{name: "zero interval", cfg: ratelimit.Config{Tokens: 5, Interval: 0}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

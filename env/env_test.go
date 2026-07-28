package env

import "testing"

func TestGet(t *testing.T) {
	t.Setenv("FOO_TEST_GET", "bar")
	if got := NewEnv().Get("FOO_TEST_GET"); got != "bar" {
		t.Errorf("Get() = %q, want %q", got, "bar")
	}
	if got := NewEnv().Get("FOO_TEST_GET_UNSET"); got != "" {
		t.Errorf("Get() of unset var = %q, want empty", got)
	}
}

func TestGetOrDefault(t *testing.T) {
	e := NewEnv()

	t.Setenv("FOO_TEST_DEFAULT", "value")
	if got := e.GetOrDefault("FOO_TEST_DEFAULT", "fallback"); got != "value" {
		t.Errorf("GetOrDefault() = %q, want %q", got, "value")
	}

	if got := e.GetOrDefault("FOO_TEST_DEFAULT_UNSET", "fallback"); got != "fallback" {
		t.Errorf("GetOrDefault() = %q, want %q", got, "fallback")
	}

	t.Setenv("FOO_TEST_DEFAULT_EMPTY", "")
	if got := e.GetOrDefault("FOO_TEST_DEFAULT_EMPTY", "fallback"); got != "fallback" {
		t.Errorf("GetOrDefault() with empty env value = %q, want %q", got, "fallback")
	}
}

func TestGetBool(t *testing.T) {
	e := NewEnv()

	cases := []struct {
		name   string
		envVal string
		setEnv bool
		def    bool
		want   bool
	}{
		{"unset uses default true", "", false, true, true},
		{"unset uses default false", "", false, false, false},
		{"true", "true", true, false, true},
		{"1", "1", true, false, true},
		{"false", "false", true, true, false},
		{"invalid uses default", "not-a-bool", true, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := "FOO_TEST_BOOL_" + tc.name
			if tc.setEnv {
				t.Setenv(key, tc.envVal)
			}
			if got := e.GetBool(key, tc.def); got != tc.want {
				t.Errorf("GetBool() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGetInt(t *testing.T) {
	e := NewEnv()

	t.Setenv("FOO_TEST_INT", "42")
	if got := e.GetInt("FOO_TEST_INT", -1); got != 42 {
		t.Errorf("GetInt() = %d, want 42", got)
	}

	if got := e.GetInt("FOO_TEST_INT_UNSET", 7); got != 7 {
		t.Errorf("GetInt() unset = %d, want 7", got)
	}

	t.Setenv("FOO_TEST_INT_INVALID", "abc")
	if got := e.GetInt("FOO_TEST_INT_INVALID", 99); got != 99 {
		t.Errorf("GetInt() invalid = %d, want default 99", got)
	}
}

func TestGetEnvironmentName(t *testing.T) {
	e := NewEnv()

	t.Setenv("APP_ENV", "")
	if got := e.GetEnvironmentName(); got != "local" {
		t.Errorf("GetEnvironmentName() unset = %q, want %q", got, "local")
	}

	t.Setenv("APP_ENV", "staging")
	if got := e.GetEnvironmentName(); got != "staging" {
		t.Errorf("GetEnvironmentName() = %q, want %q", got, "staging")
	}
}

func TestIsProduction(t *testing.T) {
	e := NewEnv()

	t.Setenv("APP_ENV", "production")
	if !e.IsProduction() {
		t.Error("IsProduction() = false, want true")
	}

	t.Setenv("APP_ENV", "staging")
	if e.IsProduction() {
		t.Error("IsProduction() = true, want false")
	}
}

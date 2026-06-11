package admin

import (
	"context"
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := []struct {
		in      string
		want    CleanupMode
		wantErr bool
	}{
		{"", ModeSoft, false}, // default
		{"soft", ModeSoft, false},
		{"hard", ModeHard, false},
		{"SOFT", "", true},      // case-sensitive
		{"permanent", "", true}, // unknown
		{"yes", "", true},       // unknown
	}
	for _, tc := range cases {
		got, err := ParseMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseMode(%q) = %q, nil; want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseMode(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseMode(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestInPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		n    int
		want string
	}{
		{"zero", 0, ""},
		{"one", 1, "?"},
		{"two", 2, "?,?"},
		{"five", 5, "?,?,?,?,?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inPlaceholders(tc.n)
			if got != tc.want {
				t.Errorf("inPlaceholders(%d) = %q; want %q", tc.n, got, tc.want)
			}
		})
	}
}

// TestRegistry_DuplicateRegisterPanics locks down the boot-time guarantee:
// two diagnostics with the same name is a programming error, not a
// silent override.
func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	r := New(nil, nil)
	d := &fakeDiagnostic{name: "foo"}
	r.Register(d)

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatalf("registering a duplicate name should panic, got nothing")
		}
		if !strings.Contains(rec.(string), "registered twice") {
			t.Errorf("panic message = %q; want contains 'registered twice'", rec)
		}
	}()
	r.Register(&fakeDiagnostic{name: "foo"})
}

func TestRegistry_GetReturnsNilForUnknown(t *testing.T) {
	r := New(nil, nil)
	if r.Get("unknown") != nil {
		t.Errorf("Get(unknown) should return nil")
	}
}

// fakeDiagnostic is the smallest Diagnostic for registry-shape tests
// that don't actually need to detect or clean anything.
type fakeDiagnostic struct{ name string }

func (f *fakeDiagnostic) Name() string                            { return f.name }
func (f *fakeDiagnostic) Description() string                     { return "" }
func (f *fakeDiagnostic) Detect(_ context.Context) ([]Row, error) { return nil, nil }
func (f *fakeDiagnostic) Cleanup(_ context.Context, _ CleanupRequest) (CleanupResult, error) {
	return CleanupResult{}, nil
}

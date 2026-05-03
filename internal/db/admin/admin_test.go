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
		{"", ModeSoft, false},        // default
		{"soft", ModeSoft, false},
		{"hard", ModeHard, false},
		{"SOFT", "", true},           // case-sensitive
		{"permanent", "", true},      // unknown
		{"yes", "", true},            // unknown
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

func TestPgArray(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", []string{}, "{}"},
		{"single", []string{"abc"}, `{"abc"}`},
		{"multiple", []string{"abc", "def"}, `{"abc","def"}`},
		{"with-quote", []string{`abc"xyz`}, `{"abc\"xyz"}`},
		{"hex hashes", []string{"6eefc7ca2759951a4f79de65825f80f48040d6e0", "4980ae45cac9000000000000000000000000aaaa"},
			`{"6eefc7ca2759951a4f79de65825f80f48040d6e0","4980ae45cac9000000000000000000000000aaaa"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pgArray(tc.in)
			if got != tc.want {
				t.Errorf("pgArray(%v) = %q; want %q", tc.in, got, tc.want)
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

func (f *fakeDiagnostic) Name() string                                                  { return f.name }
func (f *fakeDiagnostic) Description() string                                           { return "" }
func (f *fakeDiagnostic) Detect(_ context.Context) ([]Row, error)                       { return nil, nil }
func (f *fakeDiagnostic) Cleanup(_ context.Context, _ CleanupRequest) (CleanupResult, error) { return CleanupResult{}, nil }

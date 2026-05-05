package iac

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParse_MinimalProjectName(t *testing.T) {
	input := []byte("project_name: myapp\nexpose:\n  service: app\n  port: 80\n")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &Config{
		ProjectName: "myapp",
		Expose:      ExposeSpec{Service: "app", Port: 80},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestParse_ProjectNameRequired(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"missing entirely", "expose:\n  service: app\n  port: 80\n"},
		{"empty string", "project_name: \"\"\nexpose:\n  service: app\n  port: 80\n"},
		{"whitespace only", "project_name: \"   \"\nexpose:\n  service: app\n  port: 80\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error for input %q, got nil", tc.input)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
			if !strings.Contains(err.Error(), "project_name") {
				t.Fatalf("expected error to mention project_name, got %q", err.Error())
			}
		})
	}
}

func TestParse_UnknownFieldsRejected(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{
			"unknown top-level",
			"project_name: app\nexpose:\n  service: app\n  port: 80\nuknown_field: 1\n",
		},
		{
			"typo in domains.preview",
			`project_name: app
expose:
  service: app
  port: 80
domains:
  preveiw:
    pattern: "{branch}.example.com"
`,
		},
		{
			"typo in expose",
			`project_name: app
expose:
  servce: app
  port: 80
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected unknown-field error, got nil")
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
		})
	}
}

func TestParse_ExposeRequired(t *testing.T) {
	input := []byte("project_name: app\n")
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected expose-required error, got nil")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "expose") {
		t.Fatalf("expected expose in error, got %q", err.Error())
	}
}

func TestParse_ExposeValidation(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			"missing service",
			"project_name: app\nexpose:\n  port: 80\n",
			"expose.service",
		},
		{
			"empty service",
			"project_name: app\nexpose:\n  service: \"\"\n  port: 80\n",
			"expose.service",
		},
		{
			"port zero",
			"project_name: app\nexpose:\n  service: app\n  port: 0\n",
			"expose.port",
		},
		{
			"port negative",
			"project_name: app\nexpose:\n  service: app\n  port: -1\n",
			"expose.port",
		},
		{
			"port too high",
			"project_name: app\nexpose:\n  service: app\n  port: 65536\n",
			"expose.port",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestParse_ExposePortBoundaries(t *testing.T) {
	cases := []string{
		"project_name: app\nexpose:\n  service: app\n  port: 1\n",
		"project_name: app\nexpose:\n  service: app\n  port: 65535\n",
	}
	for _, input := range cases {
		if _, err := Parse([]byte(input)); err != nil {
			t.Fatalf("expected boundary input to parse, got %v", err)
		}
	}
}

func TestParse_DomainsProdHappyPath(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
domains:
  prod:
    - blocksweb.nl
    - www.blocksweb.nl
    - api.example.co.uk
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"blocksweb.nl", "www.blocksweb.nl", "api.example.co.uk"}
	if !reflect.DeepEqual(got.Domains.Prod, want) {
		t.Fatalf("got %v want %v", got.Domains.Prod, want)
	}
}

func TestParse_DomainsProdRejectsInvalid(t *testing.T) {
	cases := []struct {
		name  string
		entry string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"contains space", "bad domain.com"},
		{"trailing dot", "blocksweb.nl."},
		{"leading dot", ".blocksweb.nl"},
		{"underscore", "bad_label.com"},
		{"label too long", strings.Repeat("a", 64) + ".com"},
		{"bare TLD only", "localhost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := "project_name: app\nexpose:\n  service: app\n  port: 80\ndomains:\n  prod:\n    - " + yamlQuote(tc.entry) + "\n"
			_, err := Parse([]byte(input))
			if err == nil {
				t.Fatalf("expected invalid-hostname error for %q", tc.entry)
			}
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("expected ErrInvalidConfig, got %v", err)
			}
			if !strings.Contains(err.Error(), "domains.prod") {
				t.Fatalf("expected error to mention domains.prod, got %q", err.Error())
			}
		})
	}
}

// yamlQuote wraps a value in double quotes so YAML treats it as a string
// regardless of whitespace or special characters.
func yamlQuote(s string) string {
	return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
}

func TestParse_DomainsPreviewHappyPath(t *testing.T) {
	input := []byte(`project_name: stripe-payments
expose:
  service: app
  port: 80
domains:
  preview:
    pattern: "{branch}.stripe-payments.blocksweb.nl"
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "{branch}.stripe-payments.blocksweb.nl"
	if got.Domains.Preview.Pattern != want {
		t.Fatalf("got %q want %q", got.Domains.Preview.Pattern, want)
	}
}

func TestParse_DomainsPreviewOptional(t *testing.T) {
	// Omitting domains.preview entirely is fine.
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Domains.Preview.Pattern != "" {
		t.Fatalf("expected empty pattern, got %q", got.Domains.Preview.Pattern)
	}
}

func TestParse_DomainsPreviewRequiresBranchPlaceholder(t *testing.T) {
	input := []byte(`project_name: app
expose:
  service: app
  port: 80
domains:
  preview:
    pattern: "preview.example.com"
`)
	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected error for missing {branch}, got nil")
	}
	if !strings.Contains(err.Error(), "{branch}") {
		t.Fatalf("expected error to mention {branch}, got %q", err.Error())
	}
}

func TestParse_DomainsPreviewMustFormValidHostname(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
	}{
		{"trailing dot", "{branch}.example.com."},
		{"underscore", "{branch}_preview.example.com"},
		{"bare branch placeholder", "{branch}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf(`project_name: app
expose:
  service: app
  port: 80
domains:
  preview:
    pattern: %q
`, tc.pattern)
			_, err := Parse([]byte(input))
			if err == nil {
				t.Fatalf("expected invalid-hostname error for %q", tc.pattern)
			}
			if !strings.Contains(err.Error(), "domains.preview.pattern") {
				t.Fatalf("expected error to mention domains.preview.pattern, got %q", err.Error())
			}
		})
	}
}

func TestParse_ServicesBlock(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want Services
	}{
		{
			"both enabled",
			`project_name: app
expose:
  service: app
  port: 80
services:
  postgres: true
  redis: true
`,
			Services{Postgres: true, Redis: true},
		},
		{
			"postgres only",
			`project_name: app
expose:
  service: app
  port: 80
services:
  postgres: true
`,
			Services{Postgres: true, Redis: false},
		},
		{
			"redis only",
			`project_name: app
expose:
  service: app
  port: 80
services:
  redis: true
`,
			Services{Postgres: false, Redis: true},
		},
		{
			"both omitted (services key missing)",
			`project_name: app
expose:
  service: app
  port: 80
`,
			Services{},
		},
		{
			"both explicitly false",
			`project_name: app
expose:
  service: app
  port: 80
services:
  postgres: false
  redis: false
`,
			Services{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Services != tc.want {
				t.Fatalf("got %+v want %+v", got.Services, tc.want)
			}
		})
	}
}

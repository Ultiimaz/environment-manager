package iac

import (
	"errors"
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

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

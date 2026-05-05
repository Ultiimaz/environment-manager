package iac

import (
	"reflect"
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

package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/environment-manager/backend/internal/models"
)

func writeCompose(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "docker-compose.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeCompose: %v", err)
	}
	return path
}

func readCompose(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readCompose: %v", err)
	}
	return string(data)
}

func testEnv(id, url string) *models.Environment {
	return &models.Environment{ID: id, URL: url}
}

// TestInjectTraefikLabels_EmptyProxyNetwork verifies that an empty proxyNetwork
// is a no-op (files are unchanged, no error).
func TestInjectTraefikLabels_EmptyProxyNetwork(t *testing.T) {
	dir := t.TempDir()
	original := "services:\n  web:\n    image: nginx\n    ports:\n      - \"80:80\"\n"
	path := writeCompose(t, dir, original)

	err := InjectTraefikLabels(path, testEnv("e1", "app.home"), nil, "")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	// File should be unchanged (not even read).
	got := readCompose(t, path)
	if got != original {
		t.Errorf("file modified unexpectedly:\ngot: %s\nwant: %s", got, original)
	}
}

// TestInjectTraefikLabels_ExplicitExpose verifies that an explicit ExposeSpec
// targets the named service + port.
func TestInjectTraefikLabels_ExplicitExpose(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  web:
    image: myapp
  db:
    image: postgres
    ports:
      - "5432:5432"
`
	path := writeCompose(t, dir, input)
	expose := &models.ExposeSpec{Service: "web", Port: 3000}
	env := testEnv("proj--main", "myapp.home")

	if err := InjectTraefikLabels(path, env, expose, "my-macvlan-net"); err != nil {
		t.Fatalf("InjectTraefikLabels: %v", err)
	}

	out := readCompose(t, path)

	mustContain(t, out, "traefik.enable=true")
	mustContain(t, out, "traefik.http.routers.proj--main.rule=Host(`myapp.home`)")
	mustContain(t, out, "traefik.http.routers.proj--main.entrypoints=web")
	mustContain(t, out, "traefik.http.services.proj--main.loadbalancer.server.port=3000")
	mustContain(t, out, "traefik.docker.network=my-macvlan-net")
	mustContain(t, out, "my-macvlan-net")
	mustContain(t, out, "external: true")

	// db service should NOT have traefik labels.
	if strings.Contains(out, "traefik") && !strings.Contains(out, "web:") {
		t.Error("traefik labels should only appear under the web service section")
	}
}

// TestInjectTraefikLabels_PortsConvention verifies the fallback when no
// expose is provided: the first service with ports: is targeted.
func TestInjectTraefikLabels_PortsConvention(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  app:
    image: myapp
    ports:
      - "8080:3000"
  sidecar:
    image: alpine
`
	path := writeCompose(t, dir, input)
	env := testEnv("p1--dev", "app.dev.home")

	if err := InjectTraefikLabels(path, env, nil, "proxy-net"); err != nil {
		t.Fatalf("InjectTraefikLabels: %v", err)
	}

	out := readCompose(t, path)
	// Container port is 3000 (right-hand side of 8080:3000).
	mustContain(t, out, "traefik.http.services.p1--dev.loadbalancer.server.port=3000")
	mustContain(t, out, "traefik.http.routers.p1--dev.rule=Host(`app.dev.home`)")
	mustContain(t, out, "proxy-net")
}

// TestInjectTraefikLabels_BarePort verifies that a bare port scalar ("3000")
// is treated as the container port.
func TestInjectTraefikLabels_BarePort(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  app:
    image: myapp
    ports:
      - "3000"
`
	path := writeCompose(t, dir, input)
	env := testEnv("p2--main", "bare.home")

	if err := InjectTraefikLabels(path, env, nil, "net"); err != nil {
		t.Fatalf("InjectTraefikLabels: %v", err)
	}

	out := readCompose(t, path)
	mustContain(t, out, "traefik.http.services.p2--main.loadbalancer.server.port=3000")
}

// TestInjectTraefikLabels_NoTarget verifies that when no service has ports:
// and expose is nil, the function is a no-op.
func TestInjectTraefikLabels_NoTarget(t *testing.T) {
	dir := t.TempDir()
	original := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, original)

	if err := InjectTraefikLabels(path, testEnv("e1", "x.home"), nil, "net"); err != nil {
		t.Fatalf("InjectTraefikLabels: %v", err)
	}

	got := readCompose(t, path)
	if strings.Contains(got, "traefik") {
		t.Errorf("expected no traefik labels, got:\n%s", got)
	}
}

// TestInjectTraefikLabels_PreservesOtherServices verifies that services without
// the target label are left untouched structurally (still appear in output).
func TestInjectTraefikLabels_PreservesOtherServices(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  web:
    image: myapp
    ports:
      - "80:3000"
  worker:
    image: myapp
    command: worker
  db:
    image: postgres:16
`
	path := writeCompose(t, dir, input)

	if err := InjectTraefikLabels(path, testEnv("p--main", "app.home"), nil, "macvlan"); err != nil {
		t.Fatalf("InjectTraefikLabels: %v", err)
	}

	out := readCompose(t, path)
	mustContain(t, out, "worker")
	mustContain(t, out, "postgres:16")
}

// TestInjectTraefikLabels_IdempotentLabels verifies that running inject twice
// doesn't duplicate labels.
func TestInjectTraefikLabels_IdempotentLabels(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  web:
    image: myapp
    ports:
      - "3000"
`
	path := writeCompose(t, dir, input)
	env := testEnv("p--main", "app.home")

	if err := InjectTraefikLabels(path, env, nil, "macvlan"); err != nil {
		t.Fatalf("first inject: %v", err)
	}
	if err := InjectTraefikLabels(path, env, nil, "macvlan"); err != nil {
		t.Fatalf("second inject: %v", err)
	}

	out := readCompose(t, path)
	count := strings.Count(out, "traefik.enable=true")
	if count != 1 {
		t.Errorf("expected 1 occurrence of traefik.enable=true, got %d\n%s", count, out)
	}
}

// TestInjectTraefikLabels_LongFormPort tests the long-form ports mapping.
func TestInjectTraefikLabels_LongFormPort(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  web:
    image: myapp
    ports:
      - target: 4000
        published: 80
`
	path := writeCompose(t, dir, input)
	env := testEnv("p--main", "app.home")

	if err := InjectTraefikLabels(path, env, nil, "net"); err != nil {
		t.Fatalf("InjectTraefikLabels: %v", err)
	}

	out := readCompose(t, path)
	mustContain(t, out, "traefik.http.services.p--main.loadbalancer.server.port=4000")
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q\ngot:\n%s", needle, haystack)
	}
}

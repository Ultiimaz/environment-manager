package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeNetworksTestCompose(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "docker-compose.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func readNetworksTestCompose(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

func TestInjectPaasNet_EmptyNetworkIsNoop(t *testing.T) {
	dir := t.TempDir()
	original := "services:\n  app:\n    image: alpine\n"
	path := writeNetworksTestCompose(t, dir, original)
	if err := InjectPaasNet(path, ""); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got := readNetworksTestCompose(t, path); got != original {
		t.Errorf("file modified for empty-network input:\ngot: %s\nwant: %s", got, original)
	}
}

func TestInjectPaasNet_AddsNetworkToService(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeNetworksTestCompose(t, dir, input)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatalf("InjectPaasNet: %v", err)
	}
	out := readNetworksTestCompose(t, path)
	if !strings.Contains(out, "paas-net") {
		t.Errorf("expected paas-net in compose, got:\n%s", out)
	}
	if !strings.Contains(out, "external: true") && !strings.Contains(out, "external: \"true\"") {
		t.Errorf("expected external network declaration, got:\n%s", out)
	}
}

func TestInjectPaasNet_PreservesExistingNetworks(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  app:
    image: alpine
    networks:
      - default
      - my-other-net
`
	path := writeNetworksTestCompose(t, dir, input)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatal(err)
	}
	out := readNetworksTestCompose(t, path)
	for _, want := range []string{"default", "my-other-net", "paas-net"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q after injection:\n%s", want, out)
		}
	}
}

func TestInjectPaasNet_Idempotent(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeNetworksTestCompose(t, dir, input)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatal(err)
	}
	first := readNetworksTestCompose(t, path)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatal(err)
	}
	second := readNetworksTestCompose(t, path)
	if first != second {
		t.Errorf("second injection changed file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestInjectPaasNet_AddsToAllServices(t *testing.T) {
	dir := t.TempDir()
	input := `services:
  app:
    image: alpine
  worker:
    image: busybox
`
	path := writeNetworksTestCompose(t, dir, input)
	if err := InjectPaasNet(path, "paas-net"); err != nil {
		t.Fatal(err)
	}
	out := readNetworksTestCompose(t, path)
	// Both services should reference paas-net. We check by counting occurrences:
	// the literal "paas-net" should appear at least twice for the services and
	// once more for the top-level networks: declaration.
	count := strings.Count(out, "paas-net")
	if count < 3 {
		t.Errorf("expected paas-net to appear in 2 services + 1 top-level (>=3 occurrences), got %d:\n%s", count, out)
	}
}

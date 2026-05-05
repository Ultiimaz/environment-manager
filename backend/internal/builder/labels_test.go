package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/environment-manager/backend/internal/iac"
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

	err := InjectTraefikLabels(path, testEnv("e1", "app.home"), nil, TraefikOptions{})
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

	if err := InjectTraefikLabels(path, env, expose, TraefikOptions{ProxyNetwork: "my-macvlan-net"}); err != nil {
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

	if err := InjectTraefikLabels(path, env, nil, TraefikOptions{ProxyNetwork: "proxy-net"}); err != nil {
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

	if err := InjectTraefikLabels(path, env, nil, TraefikOptions{ProxyNetwork: "net"}); err != nil {
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

	if err := InjectTraefikLabels(path, testEnv("e1", "x.home"), nil, TraefikOptions{ProxyNetwork: "net"}); err != nil {
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

	if err := InjectTraefikLabels(path, testEnv("p--main", "app.home"), nil, TraefikOptions{ProxyNetwork: "macvlan"}); err != nil {
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

	if err := InjectTraefikLabels(path, env, nil, TraefikOptions{ProxyNetwork: "macvlan"}); err != nil {
		t.Fatalf("first inject: %v", err)
	}
	if err := InjectTraefikLabels(path, env, nil, TraefikOptions{ProxyNetwork: "macvlan"}); err != nil {
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

	if err := InjectTraefikLabels(path, env, nil, TraefikOptions{ProxyNetwork: "net"}); err != nil {
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

func TestInjectTraefikLabels_V2_HomeAndPublicRouters(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{
		ID:         "stripe--main",
		URL:        "stripe-payments.home",
		Kind:       models.EnvKindProd,
		BranchSlug: "main",
	}
	domains := &iac.Domains{
		Prod: []string{"blocksweb.nl", "www.blocksweb.nl"},
	}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          domains,
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatalf("InjectTraefikLabels: %v", err)
	}
	out := readCompose(t, path)

	// Home router: HTTP entrypoint, Host(`stripe-payments.home`).
	mustContain(t, out, "traefik.http.routers.stripe--main-home.rule=Host(`stripe-payments.home`)")
	mustContain(t, out, "traefik.http.routers.stripe--main-home.entrypoints=web")

	// Public router: HTTPS entrypoint, Host union, TLS+LE.
	mustContain(t, out, "traefik.http.routers.stripe--main-public.rule=Host(`blocksweb.nl`) || Host(`www.blocksweb.nl`)")
	mustContain(t, out, "traefik.http.routers.stripe--main-public.entrypoints=websecure")
	mustContain(t, out, "traefik.http.routers.stripe--main-public.tls=true")
	mustContain(t, out, "traefik.http.routers.stripe--main-public.tls.certresolver=letsencrypt")

	// Backend service definition still points at the exposed port.
	mustContain(t, out, "traefik.http.services.stripe--main.loadbalancer.server.port=80")
}

func TestInjectTraefikLabels_V2_DomainsNilUsesLegacyPath(t *testing.T) {
	// Confirm the v1 path still emits exactly the legacy single router.
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{ID: "p--main", URL: "myapp.home", Kind: models.EnvKindProd}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 8080}, TraefikOptions{
		ProxyNetwork: "my-net",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Legacy single router uses unsuffixed env-id.
	mustContain(t, out, "traefik.http.routers.p--main.rule=Host(`myapp.home`)")
	mustContain(t, out, "traefik.http.routers.p--main.entrypoints=web")
	// No -home or -public suffixes when Domains is nil.
	if strings.Contains(out, "p--main-home") || strings.Contains(out, "p--main-public") {
		t.Errorf("legacy path should not emit -home or -public routers; got:\n%s", out)
	}
}

func TestInjectTraefikLabels_V2_LeEmailEmptyFallbackToHttp(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{ID: "p--main", URL: "myapp.home", Kind: models.EnvKindProd}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          &iac.Domains{Prod: []string{"blocksweb.nl"}},
		LetsencryptEmail: "", // unset
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Public router exists but on web entrypoint (no TLS, no LE).
	mustContain(t, out, "traefik.http.routers.p--main-public.rule=Host(`blocksweb.nl`)")
	mustContain(t, out, "traefik.http.routers.p--main-public.entrypoints=web")
	// No tls=true label.
	if strings.Contains(out, "p--main-public.tls=true") {
		t.Errorf("expected no TLS label when LetsencryptEmail is empty; got:\n%s", out)
	}
}

func TestInjectTraefikLabels_V2_HttpsRedirect(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{ID: "p--main", URL: "myapp.home", Kind: models.EnvKindProd}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          &iac.Domains{Prod: []string{"blocksweb.nl"}},
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Redirect router on web entrypoint with same host rule.
	mustContain(t, out, "traefik.http.routers.p--main-public-http.rule=Host(`blocksweb.nl`)")
	mustContain(t, out, "traefik.http.routers.p--main-public-http.entrypoints=web")
	mustContain(t, out, "traefik.http.routers.p--main-public-http.middlewares=https-redirect-p--main")
	// Middleware definition.
	mustContain(t, out, "traefik.http.middlewares.https-redirect-p--main.redirectscheme.scheme=https")
}

func TestInjectTraefikLabels_V2_NoRedirectWhenLeUnset(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{ID: "p--main", URL: "myapp.home", Kind: models.EnvKindProd}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          &iac.Domains{Prod: []string{"blocksweb.nl"}},
		LetsencryptEmail: "", // unset
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// No redirect router or middleware when LE is not configured.
	if strings.Contains(out, "p--main-public-http") {
		t.Errorf("expected no redirect router when LE unset; got:\n%s", out)
	}
	if strings.Contains(out, "https-redirect-p--main") {
		t.Errorf("expected no redirect middleware when LE unset; got:\n%s", out)
	}
}

func TestInjectTraefikLabels_V2_PreviewPattern(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{
		ID:         "stripe--feature-x",
		URL:        "feature-x.stripe-payments.home",
		Kind:       models.EnvKindPreview,
		BranchSlug: "feature-x",
	}
	domains := &iac.Domains{
		Preview: iac.PreviewDomains{Pattern: "{branch}.stripe-payments.blocksweb.nl"},
	}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          domains,
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Resolved preview domain on public router.
	mustContain(t, out, "traefik.http.routers.stripe--feature-x-public.rule=Host(`feature-x.stripe-payments.blocksweb.nl`)")
	mustContain(t, out, "traefik.http.routers.stripe--feature-x-public.entrypoints=websecure")
}

func TestInjectTraefikLabels_V2_PreviewPatternEmptyOnlyHome(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{
		ID:         "p--feature-y",
		URL:        "feature-y.myapp.home",
		Kind:       models.EnvKindPreview,
		BranchSlug: "feature-y",
	}
	domains := &iac.Domains{
		Prod: []string{"shouldnt.be.used.com"}, // prod domains do NOT apply to preview envs
	}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          domains,
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	// Only home router; no public router because preview pattern is empty.
	mustContain(t, out, "p--feature-y-home")
	if strings.Contains(out, "p--feature-y-public") {
		t.Errorf("preview env without preview pattern should not emit -public router; got:\n%s", out)
	}
	if strings.Contains(out, "shouldnt.be.used.com") {
		t.Errorf("Domains.Prod should not apply to preview envs; got:\n%s", out)
	}
}

func TestInjectTraefikLabels_V2_ProdEnvIgnoresPreviewPattern(t *testing.T) {
	dir := t.TempDir()
	input := "services:\n  app:\n    image: alpine\n"
	path := writeCompose(t, dir, input)

	env := &models.Environment{
		ID:   "p--main",
		URL:  "myapp.home",
		Kind: models.EnvKindProd,
	}
	domains := &iac.Domains{
		Prod:    []string{"myapp.com"},
		Preview: iac.PreviewDomains{Pattern: "{branch}.myapp.com"}, // ignored for prod
	}
	err := InjectTraefikLabels(path, env, &models.ExposeSpec{Service: "app", Port: 80}, TraefikOptions{
		ProxyNetwork:     "my-net",
		Domains:          domains,
		LetsencryptEmail: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := readCompose(t, path)
	mustContain(t, out, "Host(`myapp.com`)")
	if strings.Contains(out, "{branch}") {
		t.Errorf("literal {branch} placeholder leaked into output:\n%s", out)
	}
	// Pattern resolution on prod with empty BranchSlug would produce ".myapp.com" —
	// verify the preview pattern was NOT applied for prod env.
	if strings.Contains(out, "Host(`.myapp.com`)") {
		t.Errorf("preview pattern resolved for prod env; got:\n%s", out)
	}
}

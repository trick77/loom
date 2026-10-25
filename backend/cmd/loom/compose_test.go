package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestComposePassesImageGenerationEnv(t *testing.T) {
	for _, path := range []string{
		"../../../compose.yaml",
		"../../../compose.dev.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			compose := string(data)

			for _, want := range []string{
				`BACKEND_IMAGE_GEN_BASE_URL: "${BACKEND_IMAGE_GEN_BASE_URL:-https://queue.fal.run}"`,
				`BACKEND_IMAGE_GEN_API_KEY: "${BACKEND_IMAGE_GEN_API_KEY:-}"`,
				`BACKEND_IMAGE_GEN_MODEL: "${BACKEND_IMAGE_GEN_MODEL:-fal-ai/flux-2-pro}"`,
				`BACKEND_IMAGE_GEN_TYPOGRAPHY_MODEL: "${BACKEND_IMAGE_GEN_TYPOGRAPHY_MODEL:-fal-ai/flux-2-max}"`,
				`BACKEND_IMAGE_GEN_POLL_TIMEOUT: "${BACKEND_IMAGE_GEN_POLL_TIMEOUT:-1m}"`,
			} {
				if !strings.Contains(compose, want) {
					t.Fatalf("%s does not pass %s into the loom container", path, strings.Split(want, ":")[0])
				}
			}
		})
	}
}

// readsImageRefFromEnv reports whether a file reads a container-image ref from
// the environment, which production must not do. It is a substring scan over
// "BACKEND_IMAGE" and friends, so it also catches names not invented yet — but
// BACKEND_IMAGE_GEN_* is the image *generation* config, unrelated to image refs,
// so that prefix is removed before scanning rather than exempted by pattern
// (an exemption written into the pattern would let a real BACKEND_IMAGE_TAG
// override through as well).
func readsImageRefFromEnv(content []byte) bool {
	scan := strings.ReplaceAll(string(content), "BACKEND_IMAGE_GEN_", "")
	for _, name := range []string{"BACKEND_IMAGE", "BACKEND_UI_IMAGE", "BACKEND_FETCH_IMAGE", "BACKEND_OBSCURA_IMAGE"} {
		if strings.Contains(scan, name) {
			return true
		}
	}
	return false
}

func TestProductionComposeUsesPrebuiltImages(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)

	if strings.Contains(compose, "\n    build:") {
		t.Fatal("compose.yaml must use prebuilt images, not local build directives")
	}
	if readsImageRefFromEnv(data) {
		t.Fatal("compose.yaml must hardcode production image refs instead of reading image refs from env")
	}

	if !strings.Contains(compose, `image: ghcr.io/trick77/loom:latest`) {
		t.Fatal("compose.yaml missing production image reference ghcr.io/trick77/loom:latest")
	}
	// Fetch runs in-process (github.com/trick77/webfetch), so there is no fetch
	// sidecar image, service, or isolated network anymore.
	if strings.Contains(compose, "loom-fetch") {
		t.Fatal("compose.yaml must not reference the removed loom-fetch image")
	}
	if strings.Contains(compose, "\n  fetch:") {
		t.Fatal("compose.yaml must not define a fetch service; fetch runs in-process")
	}
	if strings.Contains(compose, "fetch-mcp") {
		t.Fatal("compose.yaml must not reference the removed fetch-mcp network")
	}

	envExample, err := os.ReadFile("../../../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	if readsImageRefFromEnv(envExample) {
		t.Fatal(".env.example must not expose production image overrides")
	}
}

func TestObscuraUsesUpstreamNativeMCPImage(t *testing.T) {
	for _, path := range []string{
		"../../../compose.yaml",
		"../../../compose.dev.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			compose := string(data)
			service := composeService(t, compose, "obscura")

			// A concrete release, so a Dependabot bump needs no test edit but a
			// floating tag fails; v0.2.3+, the first to honour OBSCURA_MCP_TOKEN
			// (below it loom's bearer token would guard nothing).
			m := regexp.MustCompile(`(?m)^    image: h4ckf0r0day/obscura:(\d+)\.(\d+)\.(\d+)$`).FindStringSubmatch(service)
			if m == nil {
				t.Fatalf("%s obscura service must run the upstream image at a pinned version", path)
			}
			v := [3]int{}
			for i := range v {
				v[i], _ = strconv.Atoi(m[i+1])
			}
			if v[0] == 0 && (v[1] < 2 || v[1] == 2 && v[2] < 3) {
				t.Fatalf("%s: obscura %s.%s.%s predates OBSCURA_MCP_TOKEN (v0.2.3)", path, m[1], m[2], m[3])
			}
			for _, want := range []string{
				`- mcp`,
				`- --http`,
				`- --host`,
				`- 0.0.0.0`,
				`- --port`,
				`- "8090"`,
				`- --stealth`,
			} {
				if !strings.Contains(service, want) {
					t.Fatalf("%s obscura service missing native MCP fragment %q", path, want)
				}
			}
			for _, unwanted := range []string{
				"ghcr.io/trick77/loom-obscura",
				"context: ./obscura",
				"supergateway",
			} {
				if strings.Contains(service, unwanted) {
					t.Fatalf("%s obscura service must not contain wrapper fragment %q", path, unwanted)
				}
			}

			if path == "../../../compose.yaml" && !strings.Contains(service, "- loom") {
				t.Fatal("production obscura service must join the loom network")
			}
		})
	}
}

// Loom must send the token obscura expects. Production: loom reads it from
// .env via env_file (an environment entry would override that), obscura gets
// the same .env value, with no default. Dev: one literal on both sides.
func TestComposeSharesObscuraToken(t *testing.T) {
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(data)
	}

	prod := read("../../../compose.yaml")
	loom := composeService(t, prod, "loom")
	if !strings.Contains(loom, "\n      - path: .env\n") {
		t.Fatal("compose.yaml: loom must load .env through env_file")
	}
	if strings.Contains(loom, "BACKEND_OBSCURA_MCP_TOKEN:") {
		t.Fatal("compose.yaml: loom must take BACKEND_OBSCURA_MCP_TOKEN from env_file, not set it")
	}
	if !strings.Contains(composeService(t, prod, "obscura"), "\n      OBSCURA_MCP_TOKEN: \"${BACKEND_OBSCURA_MCP_TOKEN:?") {
		t.Fatal("compose.yaml: obscura must read OBSCURA_MCP_TOKEN from BACKEND_OBSCURA_MCP_TOKEN, required")
	}

	dev := read("../../../compose.dev.yaml")
	value := func(service, key string) string {
		m := regexp.MustCompile(`(?m)^      ` + key + `: (\S+)$`).FindStringSubmatch(composeService(t, dev, service))
		if m == nil {
			t.Fatalf("compose.dev.yaml: %s sets no %s", service, key)
		}
		return m[1]
	}
	if l, o := value("loom", "BACKEND_OBSCURA_MCP_TOKEN"), value("obscura", "OBSCURA_MCP_TOKEN"); l != o || len(l) < 32 {
		t.Fatalf("compose.dev.yaml: loom sends %q, obscura expects %q; want one value of 32+ characters", l, o)
	}
}

func TestProductionComposePublishesNoHostPorts(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)

	// The single loom image serves API + embedded SPA and is reached only via
	// Traefik, so no service publishes host ports (and the old nginx UI is gone).
	if strings.Contains(compose, "\n  loom-ui:") {
		t.Fatal("compose.yaml must not define a separate loom-ui service")
	}
	if strings.Contains(compose, "\n    ports:") {
		t.Fatal("no production service may publish host ports; Traefik fronts loom directly")
	}
}

func TestProductionComposeDefinesTraefikEntrypoint(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)

	loomService := composeService(t, compose, "loom")
	for _, want := range []string{
		"- traefik",
		`traefik.enable: "true"`,
		`traefik.docker.network: traefik`,
		`traefik.http.services.loom.loadbalancer.server.port: "8080"`,
		`traefik.http.routers.loom.entrypoints: websecure`,
		"Host(`${PUBLIC_FQDN:?",
		`traefik.http.routers.loom.tls: "true"`,
	} {
		if !strings.Contains(loomService, want) {
			t.Fatalf("loom service missing Traefik fragment %q", want)
		}
	}
	for _, unwanted := range []string{"certresolver", "loom-http", "redirect-to-https"} {
		if strings.Contains(loomService, unwanted) {
			t.Fatalf("loom service must not include unnecessary Traefik fragment %q", unwanted)
		}
	}

	for _, name := range []string{"tika", "obscura", "sandbox"} {
		service := composeService(t, compose, name)
		if !strings.Contains(service, `traefik.enable: "false"`) {
			t.Fatalf("%s service must disable Traefik", name)
		}
	}

	if !strings.Contains(compose, "\n  traefik:\n    external: true") {
		t.Fatal("compose.yaml must declare the external traefik network")
	}
}

func TestProductionComposeUsesNamedPrivateNetworks(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)

	if strings.Contains(compose, "\n  default:") || strings.Contains(compose, "- default") {
		t.Fatal("compose.yaml must use named private networks instead of the implicit default network")
	}

	backendService := composeService(t, compose, "loom")
	for _, want := range []string{"- traefik", "- loom"} {
		if !strings.Contains(backendService, want) {
			t.Fatalf("loom service missing network %q", want)
		}
	}

	for _, want := range []string{
		"\n  loom:",
		"\n  traefik:\n    external: true",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml missing network declaration %q", want)
		}
	}
}

func TestProductionComposeHealthchecksUseSixtySecondIntervals(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)

	for _, name := range []string{"loom", "tika", "obscura", "sandbox"} {
		service := composeService(t, compose, name)
		if !strings.Contains(service, "\n    healthcheck:") {
			t.Fatalf("%s service missing healthcheck", name)
		}
		if !strings.Contains(service, "\n      interval: 60s") {
			t.Fatalf("%s healthcheck must use interval: 60s", name)
		}
		if strings.Contains(service, "interval=10s") || strings.Contains(service, "\n      interval: 30s") {
			t.Fatalf("%s healthcheck must not use a 10s or 30s interval", name)
		}
	}

	if !strings.Contains(composeService(t, compose, "loom"), `test: ["CMD", "/loom", "healthcheck"]`) {
		t.Fatal("loom service must use the built-in /loom healthcheck command")
	}
}

func TestProductionComposeUsesPhysicalDataDirectory(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)
	service := composeService(t, compose, "loom")

	for _, want := range []string{
		`user: "1000:1000"`,
		"- ./data:/data",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("loom service missing physical data directory fragment %q", want)
		}
	}
	if strings.Contains(compose, "loom-data") {
		t.Fatal("production compose must use ./data, not the loom-data named volume")
	}
}

// The sandbox runs model-written code. Its isolation is part of compose, so
// these settings are pinned: a plain container with nothing on the host, no
// SYS_ADMIN, no egress, no data volume.
func TestProductionComposeIsolatesTheSandbox(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)
	service := composeService(t, compose, "sandbox")
	for _, want := range []string{
		"image: ghcr.io/trick77/loom-sandbox:latest",
		"read_only: true",
		"cap_drop: [ALL]",
		"- no-new-privileges:true",
		"mem_limit: 4g",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("sandbox service missing %q", want)
		}
	}
	networks := service[strings.Index(service, "\n    networks:"):]
	networks = networks[:strings.Index(networks, "\n    labels:")]
	if strings.TrimSpace(networks) != "networks:\n      - sandbox" {
		t.Fatalf("sandbox must join only the sandbox network, got:\n%s", networks)
	}
	for _, unwanted := range []string{"volumes:", "/data", "SYS_ADMIN", "runtime:", "profiles:", "privileged", "ports:"} {
		if strings.Contains(service, unwanted) {
			t.Fatalf("sandbox service must not contain %q", unwanted)
		}
	}
	if !strings.Contains(compose, "\n  sandbox:\n    internal: true") {
		t.Fatal("the sandbox network must be internal (no egress)")
	}
	loom := composeService(t, compose, "loom")
	for _, want := range []string{
		"- sandbox",
		`BACKEND_SANDBOX_URL: "http://sandbox:8070"`,
	} {
		if !strings.Contains(loom, want) {
			t.Fatalf("loom service missing %q", want)
		}
	}
	// loom must boot even when the sidecar does not.
	if strings.Contains(loom, "      sandbox:\n        condition") {
		t.Fatal("loom must not depend on the sandbox service")
	}
}

func TestBackendContainerfileBuildsAndEmbedsUI(t *testing.T) {
	// The single production image builds the Vite bundle in a node stage and copies
	// it into backend/web/dist so //go:embed all:dist bakes the real UI into the Go
	// binary. There is no longer a separate nginx UI image.
	if _, err := os.Stat("../../../ui/nginx.conf"); err == nil {
		t.Fatal("ui/nginx.conf must be removed; the backend serves the SPA directly")
	}
	if _, err := os.Stat("../../../ui/Containerfile"); err == nil {
		t.Fatal("ui/Containerfile must be removed; the backend Containerfile builds the UI")
	}
	data, err := os.ReadFile("../../../backend/Containerfile")
	if err != nil {
		t.Fatalf("read backend/Containerfile: %v", err)
	}
	dockerfile := string(data)
	if !regexp.MustCompile(`(?m)^FROM node:\S+ AS ui$`).MatchString(dockerfile) {
		t.Fatalf("backend/Containerfile missing node UI-build stage (FROM node:<tag> AS ui)")
	}
	for _, want := range []string{
		"RUN npm run build",
		"COPY --from=ui /app/backend/web/dist ./web/dist",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("backend/Containerfile missing UI-build fragment %q", want)
		}
	}
}

func composeService(t *testing.T, compose, name string) string {
	t.Helper()
	start := strings.Index(compose, "\n  "+name+":")
	if start < 0 {
		t.Fatalf("compose.yaml missing %s service", name)
	}
	rest := compose[start+1:]
	lines := strings.Split(rest, "\n")
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			return strings.Join(lines[:i], "\n")
		}
	}
	return rest
}

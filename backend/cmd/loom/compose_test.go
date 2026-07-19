package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestComposePassesBFLImageGenerationEnv(t *testing.T) {
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
				`BACKEND_BFL_BASE_URL: "${BACKEND_BFL_BASE_URL:-https://api.bfl.ai/v1}"`,
				`BACKEND_BFL_API_KEY: "${BACKEND_BFL_API_KEY:-}"`,
				`BACKEND_BFL_MODEL: "${BACKEND_BFL_MODEL:-flux-2-klein-4b}"`,
			} {
				if !strings.Contains(compose, want) {
					t.Fatalf("%s does not pass %s into the loom container", path, strings.Split(want, ":")[0])
				}
			}
		})
	}
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
	if strings.Contains(compose, "BACKEND_IMAGE") || strings.Contains(compose, "BACKEND_UI_IMAGE") ||
		strings.Contains(compose, "BACKEND_FETCH_IMAGE") || strings.Contains(compose, "BACKEND_OBSCURA_IMAGE") {
		t.Fatal("compose.yaml must hardcode production image refs instead of reading image refs from env")
	}

	for _, want := range []string{
		`image: ghcr.io/trick77/loom:latest`,
		`image: h4ckf0r0day/obscura:0.1.8`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml missing production image reference %q", want)
		}
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
	if strings.Contains(string(envExample), "BACKEND_IMAGE") || strings.Contains(string(envExample), "BACKEND_UI_IMAGE") ||
		strings.Contains(string(envExample), "BACKEND_FETCH_IMAGE") || strings.Contains(string(envExample), "BACKEND_OBSCURA_IMAGE") {
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

			for _, want := range []string{
				`image: h4ckf0r0day/obscura:0.1.8`,
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
		"`loom.trick77.com`",
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

	for _, name := range []string{"tika", "obscura"} {
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

	for _, name := range []string{"loom", "tika", "obscura"} {
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
		"COPY --from=ui /app/ui/dist ./web/dist",
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

func TestReleaseWorkflowPublishesProductionImages(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/release.yaml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(data)

	if !strings.Contains(workflow, `ghcr.io/${{ github.repository }}:${{ steps.ver.outputs.version }}`) {
		t.Fatal("release workflow missing backend image tag")
	}
	if strings.Contains(workflow, "-ui:${{ steps.ver.outputs.version }}") {
		t.Fatal("release workflow must not publish a separate loom-ui image")
	}
	if strings.Contains(workflow, "loom-obscura") || strings.Contains(workflow, "Build and push Obscura MCP image") {
		t.Fatal("release workflow must not build or publish an Obscura wrapper image")
	}
	// Fetch runs in-process now, so there is no fetch companion image.
	if strings.Contains(workflow, "loom-fetch") || strings.Contains(workflow, "-fetch:") ||
		strings.Contains(workflow, "Build and push fetch MCP image") {
		t.Fatal("release workflow must not build or publish the removed fetch image")
	}

	tagStep := strings.Index(workflow, "- name: Create and push tag")
	if tagStep < 0 {
		t.Fatal("release workflow missing final git tag step")
	}
	idx := strings.Index(workflow, "- name: Build and push backend image")
	if idx < 0 {
		t.Fatal("release workflow missing backend image build step")
	}
	if idx > tagStep {
		t.Fatal("backend image build must run before the git tag step")
	}
}

func TestReleaseWorkflowBuildsProductionImages(t *testing.T) {
	// Production images are built and pushed by the release workflow on master
	// push. The PR test workflow no longer builds Docker images (it relied on
	// release.yaml for that), so this invariant lives against release.yaml.
	data, err := os.ReadFile("../../../.github/workflows/release.yaml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(data)

	for _, want := range []string{
		`name: Build and push backend image`,
		`file: ./backend/Containerfile`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing production image build fragment %q", want)
		}
	}
	// The UI is now built inside the backend image, so there is no separate build step.
	if strings.Contains(workflow, `file: ./ui/Containerfile`) {
		t.Fatal("release workflow must not build a separate ui image")
	}
}

func TestPRWorkflowTypechecksUI(t *testing.T) {
	// vitest does not run tsc/vite build, so the PR test workflow must run the
	// UI build itself to catch type/build breakage before merge.
	data, err := os.ReadFile("../../../.github/workflows/test.yaml")
	if err != nil {
		t.Fatalf("read test workflow: %v", err)
	}
	workflow := string(data)

	if !strings.Contains(workflow, `cd ui && npm run build`) {
		t.Fatalf("test workflow missing UI build step")
	}
}

func TestCleanupWorkflowManagesOnlyTheLoomImage(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/cleanup-images.yaml")
	if err != nil {
		t.Fatalf("read cleanup workflow: %v", err)
	}
	workflow := string(data)

	if strings.Contains(workflow, "loom-obscura") {
		t.Fatal("cleanup workflow must not manage the removed Obscura wrapper image")
	}
	if strings.Contains(workflow, "loom-fetch") {
		t.Fatal("cleanup workflow must not manage the removed fetch image")
	}
	if !strings.Contains(workflow, `image-names: "loom"`) {
		t.Fatal("cleanup workflow must manage the loom image")
	}
	if strings.Contains(workflow, "loom-ui") {
		t.Fatal("cleanup workflow must not manage the removed loom-ui image")
	}
}

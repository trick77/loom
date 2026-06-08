package main

import (
	"os"
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
				`SLOPR_BFL_BASE_URL: "${SLOPR_BFL_BASE_URL:-https://api.bfl.ai/v1}"`,
				`SLOPR_BFL_API_KEY: "${SLOPR_BFL_API_KEY:-}"`,
				`SLOPR_BFL_MODEL: "${SLOPR_BFL_MODEL:-flux-2-klein-4b}"`,
			} {
				if !strings.Contains(compose, want) {
					t.Fatalf("%s does not pass %s into the slopr container", path, strings.Split(want, ":")[0])
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
	if strings.Contains(compose, "SLOPR_IMAGE") || strings.Contains(compose, "SLOPR_UI_IMAGE") ||
		strings.Contains(compose, "SLOPR_FETCH_IMAGE") || strings.Contains(compose, "SLOPR_OBSCURA_IMAGE") {
		t.Fatal("compose.yaml must hardcode production image refs instead of reading image refs from env")
	}

	for _, want := range []string{
		`image: ghcr.io/trick77/slopr:latest`,
		`image: ghcr.io/trick77/slopr-ui:latest`,
		`image: ghcr.io/trick77/slopr-fetch:latest`,
		`image: ghcr.io/trick77/slopr-obscura:latest`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml missing production image reference %q", want)
		}
	}

	envExample, err := os.ReadFile("../../../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	if strings.Contains(string(envExample), "SLOPR_IMAGE") || strings.Contains(string(envExample), "SLOPR_UI_IMAGE") ||
		strings.Contains(string(envExample), "SLOPR_FETCH_IMAGE") || strings.Contains(string(envExample), "SLOPR_OBSCURA_IMAGE") {
		t.Fatal(".env.example must not expose production image overrides")
	}
}

func TestProductionComposeOnlyPublishesUI(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)

	uiService := strings.Index(compose, "\n  slopr-ui:")
	if uiService < 0 {
		t.Fatal("compose.yaml missing slopr-ui service")
	}
	apiService := strings.Index(compose, "\n  slopr:")
	if apiService < 0 {
		t.Fatal("compose.yaml missing slopr service")
	}
	if !strings.Contains(compose[uiService:], `- "127.0.0.1:8081:80"`) {
		t.Fatal("slopr-ui must publish localhost port 8081 to container port 80")
	}
	backendServiceEnd := strings.Index(compose[apiService+1:], "\n  tika:")
	if backendServiceEnd < 0 {
		t.Fatal("compose.yaml service ordering changed; expected tika after slopr")
	}
	backendService := compose[apiService : apiService+1+backendServiceEnd]
	if strings.Contains(backendService, "\n    ports:") {
		t.Fatal("slopr backend service must not publish host ports")
	}
}

func TestProductionComposeDefinesTraefikEntrypoint(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)

	uiService := composeService(t, compose, "slopr-ui")
	for _, want := range []string{
		"- traefik",
		`traefik.enable: "true"`,
		`traefik.docker.network: traefik`,
		`traefik.http.services.slopr.loadbalancer.server.port: "80"`,
		`traefik.http.routers.slopr.entrypoints: websecure`,
		"`slopr.trick77.com`",
		`traefik.http.routers.slopr.tls: "true"`,
	} {
		if !strings.Contains(uiService, want) {
			t.Fatalf("slopr-ui service missing Traefik fragment %q", want)
		}
	}
	for _, unwanted := range []string{"certresolver", "slopr-http", "redirect-to-https"} {
		if strings.Contains(uiService, unwanted) {
			t.Fatalf("slopr-ui service must not include unnecessary Traefik fragment %q", unwanted)
		}
	}

	for _, name := range []string{"slopr", "tika", "fetch", "obscura"} {
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

	uiService := composeService(t, compose, "slopr-ui")
	for _, want := range []string{"- traefik", "- slopr"} {
		if !strings.Contains(uiService, want) {
			t.Fatalf("slopr-ui service missing network %q", want)
		}
	}

	backendService := composeService(t, compose, "slopr")
	for _, want := range []string{"- slopr", "- fetch-mcp"} {
		if !strings.Contains(backendService, want) {
			t.Fatalf("slopr service missing network %q", want)
		}
	}

	for _, want := range []string{
		"\n  slopr:",
		"\n  fetch-mcp:",
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

	for _, name := range []string{"slopr-ui", "slopr", "tika", "fetch", "obscura"} {
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

	if !strings.Contains(composeService(t, compose, "slopr"), `test: ["CMD", "/slopr", "healthcheck"]`) {
		t.Fatal("slopr service must use the built-in /slopr healthcheck command")
	}
}

func TestProductionComposeUsesPhysicalDataDirectory(t *testing.T) {
	data, err := os.ReadFile("../../../compose.yaml")
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	compose := string(data)
	service := composeService(t, compose, "slopr")

	for _, want := range []string{
		`user: "1000:1000"`,
		"- ./data:/data",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("slopr service missing physical data directory fragment %q", want)
		}
	}
	if strings.Contains(compose, "slopr-data") {
		t.Fatal("production compose must use ./data, not the slopr-data named volume")
	}
}

func TestUIContainerfileUsesSingleWorkerNginxProxy(t *testing.T) {
	for _, path := range []string{
		"../../../ui/Containerfile",
		"../../../ui/nginx.conf",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	data, err := os.ReadFile("../../../ui/nginx.conf")
	if err != nil {
		t.Fatalf("read ui/nginx.conf: %v", err)
	}
	conf := string(data)
	for _, want := range []string{
		"worker_processes 1;",
		"resolver 127.0.0.11 valid=30s ipv6=off;",
		"set $slopr_upstream http://slopr:8080;",
		"proxy_pass $slopr_upstream;",
		"proxy_buffering off;",
		"location = /health",
		"try_files $uri $uri/ /index.html;",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("ui/nginx.conf missing %q", want)
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

	for _, want := range []string{
		`ghcr.io/${{ github.repository }}:${{ steps.ver.outputs.version }}`,
		`ghcr.io/${{ github.repository }}-ui:${{ steps.ver.outputs.version }}`,
		`ghcr.io/${{ github.repository }}-fetch:${{ steps.ver.outputs.version }}`,
		`ghcr.io/${{ github.repository }}-obscura:${{ steps.ver.outputs.version }}`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing image tag %q", want)
		}
	}

	tagStep := strings.Index(workflow, "- name: Create and push tag")
	if tagStep < 0 {
		t.Fatal("release workflow missing final git tag step")
	}
	for _, imageStep := range []string{
		"- name: Build and push Slopr image",
		"- name: Build and push Slopr UI image",
		"- name: Build and push fetch MCP image",
		"- name: Build and push Obscura MCP image",
	} {
		idx := strings.Index(workflow, imageStep)
		if idx < 0 {
			t.Fatalf("release workflow missing %q", imageStep)
		}
		if idx > tagStep {
			t.Fatalf("%q must run before the git tag step", imageStep)
		}
	}
}

func TestCIWorkflowBuildsProductionImages(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/test.yaml")
	if err != nil {
		t.Fatalf("read test workflow: %v", err)
	}
	workflow := string(data)

	for _, want := range []string{
		`name: Build backend image`,
		`file: ./backend/Containerfile`,
		`name: Build UI image`,
		`file: ./ui/Containerfile`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("test workflow missing production image build fragment %q", want)
		}
	}
}

func TestReleaseWorkflowBuildsCompanionImagesOnlyWhenChanged(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/release.yaml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(data)

	for _, want := range []string{
		`echo "previous=$LATEST" >> "$GITHUB_OUTPUT"`,
		`- id: companion_changes`,
		`git diff --quiet "${{ steps.ver.outputs.previous }}"...HEAD -- fetch`,
		`git diff --quiet "${{ steps.ver.outputs.previous }}"...HEAD -- obscura`,
		`echo "fetch_changed=true" >> "$GITHUB_OUTPUT"`,
		`echo "obscura_changed=true" >> "$GITHUB_OUTPUT"`,
		`if: ${{ steps.companion_changes.outputs.fetch_changed == 'true' }}`,
		`if: ${{ steps.companion_changes.outputs.obscura_changed == 'true' }}`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing companion-change gating fragment %q", want)
		}
	}
}

func TestReleaseWorkflowBuildsCompanionImagesWhenLatestTagIsMissing(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/release.yaml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(data)

	for _, want := range []string{
		`docker buildx imagetools inspect "ghcr.io/${{ github.repository }}-fetch:latest"`,
		`docker buildx imagetools inspect "ghcr.io/${{ github.repository }}-obscura:latest"`,
		`fetch_missing=true`,
		`obscura_missing=true`,
		`if [ "$fetch_source_changed" = "true" ] || [ "$fetch_missing" = "true" ]; then`,
		`if [ "$obscura_source_changed" = "true" ] || [ "$obscura_missing" = "true" ]; then`,
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing missing-companion-image fragment %q", want)
		}
	}
}

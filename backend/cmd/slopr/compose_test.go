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

	for _, want := range []string{
		`image: "${SLOPR_IMAGE:-ghcr.io/trick77/slopr:latest}"`,
		`image: "${SLOPR_UI_IMAGE:-ghcr.io/trick77/slopr-ui:latest}"`,
		`image: "${SLOPR_FETCH_IMAGE:-ghcr.io/trick77/slopr-fetch:latest}"`,
		`image: "${SLOPR_OBSCURA_IMAGE:-ghcr.io/trick77/slopr-obscura:latest}"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml missing production image reference %q", want)
		}
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
	if !strings.Contains(compose[uiService:], `- "8080:80"`) {
		t.Fatal("slopr-ui must publish host port 8080 to container port 80")
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
		"try_files $uri $uri/ /index.html;",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("ui/nginx.conf missing %q", want)
		}
	}
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

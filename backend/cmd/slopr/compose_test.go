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
		`image: "${SLOPR_FETCH_IMAGE:-ghcr.io/trick77/slopr-fetch:latest}"`,
		`image: "${SLOPR_OBSCURA_IMAGE:-ghcr.io/trick77/slopr-obscura:latest}"`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose.yaml missing production image reference %q", want)
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

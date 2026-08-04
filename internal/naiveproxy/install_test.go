package naiveproxy

import "testing"

func TestPlanInstallIsAllowlistedAndPinned(t *testing.T) {
	plan, err := PlanInstall(PlatformLinuxAMD64, ReleaseArtifact{
		Version: "v150.0.7871.63-1",
		URL:     "https://github.com/klzgrad/forwardproxy/releases/download/v150.0.7871.63-1/caddy-linux-amd64",
		SHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Executable != FixedExecutableName || plan.Service != FixedServiceName {
		t.Fatalf("fixed names not preserved: %#v", plan)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("unexpected steps: %#v", plan.Steps)
	}
	if plan.Installable || len(plan.Blockers) == 0 {
		t.Fatalf("plan must not imply installed support: %#v", plan)
	}
}

func TestPlanInstallRejectsNetworkOrMutableInputs(t *testing.T) {
	if _, err := PlanInstall(PlatformLinuxAMD64, ReleaseArtifact{
		Version: "latest",
		URL:     "https://github.com/klzgrad/forwardproxy/releases/latest/download/caddy-linux-amd64",
		SHA256:  "bad",
	}); err == nil {
		t.Fatal("expected unpinned plan to fail")
	}
	if _, err := PlanInstall(Platform("darwin-amd64"), ReleaseArtifact{
		Version: "v150.0.7871.63-1",
		URL:     "https://example.invalid/caddy",
		SHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}); err == nil {
		t.Fatal("expected unsupported plan to fail")
	}
}

package naiveproxy

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	FixedExecutableName = "caddy-naive"
	FixedServiceName    = "uvp-naiveproxy"
)

var (
	releaseVersionRE = regexp.MustCompile(`^v[0-9]+(?:\.[0-9]+){1,3}(?:-[0-9]+)?$`)
	sha256RE         = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
)

type Platform string

const (
	PlatformLinuxAMD64 Platform = "linux-amd64"
	PlatformLinuxARM64 Platform = "linux-arm64"
)

type ReleaseArtifact struct {
	Version string
	URL     string
	SHA256  string
}

type InstallPlan struct {
	Version     string
	Platform    Platform
	Executable  string
	Service     string
	Installable bool
	Blockers    []string
	Steps       []InstallStep
}

type InstallStep struct {
	Kind   InstallStepKind
	Source string
	Target string
	SHA256 string
}

type InstallStepKind string

const (
	StepVerifyPinnedArtifact InstallStepKind = "verify_pinned_artifact"
	StepInstallExecutable    InstallStepKind = "install_executable"
	StepInstallService       InstallStepKind = "install_service"
)

func PlanInstall(p Platform, a ReleaseArtifact) (InstallPlan, error) {
	if p != PlatformLinuxAMD64 && p != PlatformLinuxARM64 {
		return InstallPlan{}, fmt.Errorf("unsupported platform %q", p)
	}
	if !releaseVersionRE.MatchString(a.Version) {
		return InstallPlan{}, errors.New("release version must be pinned, for example v150.0.7871.63-1")
	}
	if !sha256RE.MatchString(a.SHA256) {
		return InstallPlan{}, errors.New("sha256 must be a 64-character hex digest")
	}
	if !strings.HasPrefix(a.URL, "https://github.com/klzgrad/forwardproxy/releases/download/") {
		return InstallPlan{}, errors.New("artifact URL is not allowlisted")
	}
	return InstallPlan{
		Version:     a.Version,
		Platform:    p,
		Executable:  FixedExecutableName,
		Service:     FixedServiceName,
		Installable: false,
		Blockers: []string{
			"patched caddy forwardproxy artifact must be reproducibly built and pinned by version, digest, and checksum before installation is enabled",
		},
		Steps: []InstallStep{
			{Kind: StepVerifyPinnedArtifact, Source: a.URL, SHA256: strings.ToLower(a.SHA256)},
			{Kind: StepInstallExecutable, Target: FixedExecutableName, SHA256: strings.ToLower(a.SHA256)},
			{Kind: StepInstallService, Target: FixedServiceName},
		},
	}, nil
}

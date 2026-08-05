package service

import (
	"strings"
	"testing"
)

func TestXrayVersionListIsCompatibilityPinned(t *testing.T) {
	versions, err := (&ServerService{}).GetXrayVersionsCached()
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0] != PinnedXrayVersion {
		t.Fatalf("versions = %#v, want only %q", versions, PinnedXrayVersion)
	}
}

func TestUpdateXrayRejectsUnpinnedVersionBeforeSideEffects(t *testing.T) {
	err := (&ServerService{}).UpdateXray("v26.7.28")
	if err == nil || !strings.Contains(err.Error(), PinnedXrayVersion) {
		t.Fatalf("UpdateXray error = %v, want compatibility-pin rejection", err)
	}
}

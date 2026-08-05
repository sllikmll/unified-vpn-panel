package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallAndUpdateKeepRootWebBasePath(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	for _, name := range []string{"install.sh", "update.sh"} {
		body, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, `setting -webBasePath "/"`) {
			t.Fatalf("%s does not enforce root webBasePath", name)
		}
		if strings.Contains(text, "WebBasePath is missing or too short") ||
			strings.Contains(text, `config_webBasePath=$(gen_random_string`) ||
			strings.Contains(text, `XUI_WEB_BASE_PATH:-$(gen_random_string`) {
			t.Fatalf("%s still randomizes webBasePath", name)
		}
		if name == "update.sh" && strings.Contains(text, `prompt_and_setup_ssl "${existing_port}" "${existing_webBasePath}"`) {
			t.Fatalf("%s passes root webBasePath with a leading slash to SSL setup", name)
		}
	}
	smoke, err := os.ReadFile(filepath.Join(root, "deploy", "test", "smoke-noninteractive.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(smoke), `/${XUI_WEB_BASE_PATH}/`) || !strings.Contains(string(smoke), `${XUI_WEB_BASE_PATH#/}`) {
		t.Fatal("non-interactive smoke test does not normalize root webBasePath")
	}
}

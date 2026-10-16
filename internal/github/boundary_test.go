package github_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// sources returns the non-test Go files of the module, relative to its root.
func sources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "dist" || strings.HasPrefix(d.Name(), ".")) && p != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no sources found")
	}
	return files
}

// #3-AC1
func TestOnlyPackageGithubReachesGitHub(t *testing.T) {
	forbidden := regexp.MustCompile(`"gh"|"net/http"|api\.github\.com`)
	for path, src := range sources(t) {
		if strings.HasPrefix(path, "internal/github/") {
			continue
		}
		if m := forbidden.FindString(src); m != "" {
			t.Errorf("%s contains %s; only internal/github may run gh or call the API", path, m)
		}
	}
}

// #3-AC7
func TestNoTokenHandling(t *testing.T) {
	forbidden := regexp.MustCompile(`GH_TOKEN|GITHUB_TOKEN|GH_ENTERPRISE_TOKEN|"token"|auth token|hosts\.yml`)
	for path, src := range sources(t) {
		if m := forbidden.FindString(src); m != "" {
			t.Errorf("%s contains %s; crew never reads the gh token", path, m)
		}
	}
}

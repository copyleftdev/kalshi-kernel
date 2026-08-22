package releaseaudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	registryName  = "io.github.copyleftdev/kalshi-kernel"
	repositoryURL = "https://github.com/copyleftdev/kalshi-kernel"
	modulePath    = "github.com/copyleftdev/kalshi-kernel"
	publisherName = "CopyleftDev"
)

type registryManifest struct {
	Schema      string `json:"$schema"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Repository  struct {
		URL    string `json:"url"`
		Source string `json:"source"`
	} `json:"repository"`
	Packages []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Transport    struct {
			Type string `json:"type"`
		} `json:"transport"`
	} `json:"packages"`
}

type toolManifest struct {
	Server struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"server"`
	Tools []struct {
		Name  string `yaml:"name"`
		Title string `yaml:"title"`
	} `yaml:"tools"`
}

type claudePluginManifest struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	License        string `json:"license"`
	DefaultEnabled bool   `json:"defaultEnabled"`
	MCPServers     string `json:"mcpServers"`
	Author         struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"author"`
}

type claudeMarketplace struct {
	Name  string `json:"name"`
	Owner struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"owner"`
	Plugins []struct {
		Name           string `json:"name"`
		Source         string `json:"source"`
		Version        string `json:"version"`
		DefaultEnabled bool   `json:"defaultEnabled"`
	} `json:"plugins"`
}

func TestReleaseVersionsAndRegistryIdentityStayAligned(t *testing.T) {
	version := strings.TrimSpace(string(readFile(t, "VERSION")))
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`).MatchString(version) {
		t.Fatalf("VERSION %q is not semantic", version)
	}
	if module := strings.Fields(string(readFile(t, "go.mod"))); len(module) < 2 || module[0] != "module" || module[1] != modulePath {
		t.Fatalf("go.mod module must be %q", modulePath)
	}

	var registry registryManifest
	decodeJSON(t, "server.json", &registry)
	if registry.Schema != "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json" {
		t.Errorf("unexpected MCP Registry schema %q", registry.Schema)
	}
	if registry.Name != registryName || registry.Title == "" || registry.Description == "" {
		t.Errorf("incomplete registry identity: %#v", registry)
	}
	if len(registry.Description) > 100 {
		t.Errorf("registry description has %d characters, maximum is 100", len(registry.Description))
	}
	if registry.Repository.URL != repositoryURL || registry.Repository.Source != "github" {
		t.Errorf("unexpected registry repository: %#v", registry.Repository)
	}
	if registry.Version != version {
		t.Errorf("server.json version = %q, VERSION = %q", registry.Version, version)
	}
	if len(registry.Packages) != 1 {
		t.Fatalf("server.json package count = %d, want 1", len(registry.Packages))
	}
	pack := registry.Packages[0]
	if pack.RegistryType != "oci" || pack.Transport.Type != "stdio" {
		t.Errorf("unexpected registry package: %#v", pack)
	}
	wantImage := "ghcr.io/copyleftdev/kalshi-kernel:" + version
	if pack.Identifier != wantImage {
		t.Errorf("registry image = %q, want %q", pack.Identifier, wantImage)
	}

	var tools toolManifest
	decodeYAML(t, "specs/mcp-tools.yaml", &tools)
	if tools.Server.Name != "kalshi-kernel" || tools.Server.Version != version {
		t.Errorf("MCP manifest identity = %s@%s, want kalshi-kernel@%s", tools.Server.Name, tools.Server.Version, version)
	}
	for _, tool := range tools.Tools {
		if tool.Title == "" {
			t.Errorf("tool %s has no title", tool.Name)
		}
	}

	var plugin claudePluginManifest
	decodeJSON(t, ".claude-plugin/plugin.json", &plugin)
	if plugin.Name != "kalshi-kernel" || plugin.Version != version || plugin.License != "Apache-2.0" {
		t.Errorf("unexpected Claude plugin identity: %#v", plugin)
	}
	if plugin.DefaultEnabled || plugin.MCPServers != "./.mcp.json" {
		t.Errorf("Claude plugin must be disabled by default and use the reviewed MCP config: %#v", plugin)
	}
	if plugin.Author.Name != publisherName || plugin.Author.URL != repositoryURL {
		t.Errorf("unexpected Claude plugin publisher: %#v", plugin.Author)
	}

	var marketplace claudeMarketplace
	decodeJSON(t, ".claude-plugin/marketplace.json", &marketplace)
	if marketplace.Name != "kalshi-kernel" || len(marketplace.Plugins) != 1 {
		t.Fatalf("unexpected Claude marketplace: %#v", marketplace)
	}
	if marketplace.Owner.Name != publisherName || marketplace.Owner.URL != "https://github.com/copyleftdev" {
		t.Errorf("unexpected Claude marketplace owner: %#v", marketplace.Owner)
	}
	entry := marketplace.Plugins[0]
	if entry.Name != "kalshi-kernel" || entry.Source != "./" || entry.Version != version || entry.DefaultEnabled {
		t.Errorf("unexpected Claude marketplace entry: %#v", entry)
	}

	dockerfile := string(readFile(t, "Dockerfile"))
	for _, required := range []string{
		`io.modelcontextprotocol.server.name="` + registryName + `"`,
		`org.opencontainers.image.source="` + repositoryURL + `"`,
		`ENTRYPOINT ["/kalshi-kernel"]`,
		`USER 65532:65532`,
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile is missing %q", required)
		}
	}

	releaseWorkflow := string(readFile(t, ".github/workflows/release.yml"))
	if !strings.Contains(releaseWorkflow, "images: ghcr.io/copyleftdev/kalshi-kernel") {
		t.Error("release workflow does not publish the CopyleftDev container image")
	}
}

func TestPublicRepositoryDocumentsArePresentAndExplicit(t *testing.T) {
	requiredFiles := []string{
		"LICENSE", "NOTICE", "README.md", "CHANGELOG.md", "CODE_OF_CONDUCT.md",
		"CONTRIBUTING.md", "DISCLAIMER.md", "PRIVACY.md", "SECURITY.md",
		"SUPPORT.md", "THIRD_PARTY_NOTICES.md", "docs/PUBLICATION.md",
		"docs/ARCHITECTURE.md", "docs/THREAT_MODEL.md",
		".claude-plugin/plugin.json", ".claude-plugin/marketplace.json", ".mcp.json",
		"skills/kalshi-kernel-setup/SKILL.md",
	}
	for _, name := range requiredFiles {
		if info, err := os.Stat(filepath.Join(root(t), name)); err != nil || info.Size() == 0 {
			t.Errorf("required public file %s is missing or empty", name)
		}
	}

	readme := strings.ToLower(string(readFile(t, "README.md")))
	for _, required := range []string{
		"unofficial", "not affiliated", "pre-release", "not ready for trading",
		"investment, financial, legal", "third_party_notices.md",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("README is missing disclosure %q", required)
		}
	}
}

func TestRepositoryUsesCorrectProjectSpelling(t *testing.T) {
	misspelling := "ker" + "nal"
	err := filepath.WalkDir(root(t), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "bin") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if strings.Contains(strings.ToLower(entry.Name()), misspelling) {
			t.Errorf("misspelled path: %s", path)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(contents)), misspelling) {
			t.Errorf("misspelling found in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func decodeJSON(t *testing.T, name string, target any) {
	t.Helper()
	if err := json.Unmarshal(readFile(t, name), target); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

func decodeYAML(t *testing.T, name string, target any) {
	t.Helper()
	if err := yaml.Unmarshal(readFile(t, name), target); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return contents
}

func root(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve release audit path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}

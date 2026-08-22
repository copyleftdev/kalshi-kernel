package specaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type openAPIDocument struct {
	OpenAPI    string                          `yaml:"openapi"`
	Servers    []openAPIServer                 `yaml:"servers"`
	Paths      map[string]map[string]operation `yaml:"paths"`
	Components struct {
		SecuritySchemes map[string]securityScheme `yaml:"securitySchemes"`
		Schemas         map[string]schema         `yaml:"schemas"`
	} `yaml:"components"`
}

type openAPIServer struct {
	URL string `yaml:"url"`
}

type securityScheme struct {
	Type string `yaml:"type"`
	In   string `yaml:"in"`
	Name string `yaml:"name"`
}

type operation struct {
	OperationID string                `yaml:"operationId"`
	Security    []map[string][]string `yaml:"security"`
	RequestBody struct {
		Content map[string]struct {
			Schema schema `yaml:"schema"`
		} `yaml:"content"`
	} `yaml:"requestBody"`
}

type schema struct {
	Ref        string            `yaml:"$ref"`
	Type       string            `yaml:"type"`
	Required   []string          `yaml:"required"`
	Properties map[string]schema `yaml:"properties"`
	Enum       []string          `yaml:"enum"`
}

type asyncAPIDocument struct {
	AsyncAPI string `yaml:"asyncapi"`
	Servers  map[string]struct {
		Host     string           `yaml:"host"`
		Protocol string           `yaml:"protocol"`
		Security []map[string]any `yaml:"security"`
	} `yaml:"servers"`
	Channels map[string]any `yaml:"channels"`
}

type manifest struct {
	Version int `yaml:"version"`
	Tools   []struct {
		Name        string `yaml:"name"`
		Handler     string `yaml:"handler"`
		Safety      string `yaml:"safety"`
		Description string `yaml:"description"`
		Sources     []struct {
			Spec        string `yaml:"spec"`
			OperationID string `yaml:"operation_id"`
			Channel     string `yaml:"channel"`
		} `yaml:"sources"`
		Input struct {
			Properties []struct {
				Name     string   `yaml:"name"`
				Type     string   `yaml:"type"`
				Required bool     `yaml:"required"`
				Enum     []string `yaml:"enum"`
			} `yaml:"properties"`
		} `yaml:"input"`
	} `yaml:"tools"`
}

type operationLocation struct {
	Method    string
	Path      string
	Operation operation
	Document  *openAPIDocument
}

type upstreamLock struct {
	SchemaVersion int    `json:"schema_version"`
	SourceHost    string `json:"source_host"`
	Specs         []struct {
		Filename string `json:"filename"`
		URL      string `json:"url"`
		SHA256   string `json:"sha256"`
	} `json:"specs"`
}

func TestCheckedInContractsMatchUpstreamProvenanceLock(t *testing.T) {
	lock := loadJSON[upstreamLock](t, "upstream.lock.json")
	if lock.SchemaVersion != 1 || lock.SourceHost != "docs.kalshi.com" {
		t.Fatalf("unexpected upstream provenance: schema=%d host=%q", lock.SchemaVersion, lock.SourceHost)
	}
	wantFiles := map[string]bool{
		"trade.yaml": false, "market_data_ws.yaml": false, "perps.yaml": false, "perps_ws.yaml": false,
	}
	if len(lock.Specs) != len(wantFiles) {
		t.Fatalf("upstream lock has %d specs, want %d", len(lock.Specs), len(wantFiles))
	}
	for _, entry := range lock.Specs {
		if _, ok := wantFiles[entry.Filename]; !ok {
			t.Errorf("upstream lock contains unexpected filename %q", entry.Filename)
			continue
		}
		if wantFiles[entry.Filename] {
			t.Errorf("upstream lock contains duplicate filename %q", entry.Filename)
			continue
		}
		wantFiles[entry.Filename] = true
		if entry.URL != "https://docs.kalshi.com/"+sourceFilename(entry.Filename) {
			t.Errorf("unexpected source URL for %s: %q", entry.Filename, entry.URL)
		}
		contents := readSpecFile(t, entry.Filename)
		digest := sha256.Sum256(contents)
		if got := hex.EncodeToString(digest[:]); got != entry.SHA256 {
			t.Errorf("%s digest = %s, lock has %s", entry.Filename, got, entry.SHA256)
		}
	}
}

func TestUpstreamContractsHaveExpectedDialectAndSecureTransports(t *testing.T) {
	for _, name := range []string{"trade", "perps"} {
		document := loadYAML[openAPIDocument](t, name+".yaml")
		if document.OpenAPI != "3.0.0" {
			t.Errorf("%s openapi = %q, want 3.0.0", name, document.OpenAPI)
		}
		if len(document.Servers) == 0 {
			t.Errorf("%s has no REST servers", name)
		}
		for _, server := range document.Servers {
			if !strings.HasPrefix(server.URL, "https://") {
				t.Errorf("%s contains non-HTTPS server %q", name, server.URL)
			}
		}
	}

	for _, name := range []string{"market_data_ws", "perps_ws"} {
		document := loadYAML[asyncAPIDocument](t, name+".yaml")
		if document.AsyncAPI != "3.0.0" {
			t.Errorf("%s asyncapi = %q, want 3.0.0", name, document.AsyncAPI)
		}
		if len(document.Servers) == 0 {
			t.Errorf("%s has no WebSocket servers", name)
		}
		for serverName, server := range document.Servers {
			if server.Protocol != "wss" {
				t.Errorf("%s server %s protocol = %q, want wss", name, serverName, server.Protocol)
			}
			if server.Host == "" || len(server.Security) == 0 {
				t.Errorf("%s server %s must have a host and handshake security", name, serverName)
			}
		}
	}
}

func TestOpenAPIOperationIDsAreUniqueAndMutationsRequireAuthentication(t *testing.T) {
	for _, name := range []string{"trade", "perps"} {
		document := loadYAML[openAPIDocument](t, name+".yaml")
		seen := make(map[string]string)
		for path, pathItem := range document.Paths {
			for method, operation := range pathItem {
				method = strings.ToLower(method)
				if !isHTTPMethod(method) {
					continue
				}
				location := strings.ToUpper(method) + " " + path
				if operation.OperationID == "" {
					t.Errorf("%s %s has no operationId", name, location)
					continue
				}
				if previous, exists := seen[operation.OperationID]; exists {
					t.Errorf("%s operationId %s is duplicated at %s and %s", name, operation.OperationID, previous, location)
				}
				seen[operation.OperationID] = location
				if method != "get" && method != "head" && len(operation.Security) == 0 {
					t.Errorf("%s mutation %s (%s) has no operation-level security", name, location, operation.OperationID)
				}
			}
		}
	}
}

func TestRESTAuthenticationContractIsStable(t *testing.T) {
	want := map[string]string{
		"kalshiAccessKey":       "KALSHI-ACCESS-KEY",
		"kalshiAccessSignature": "KALSHI-ACCESS-SIGNATURE",
		"kalshiAccessTimestamp": "KALSHI-ACCESS-TIMESTAMP",
	}
	for _, name := range []string{"trade", "perps"} {
		document := loadYAML[openAPIDocument](t, name+".yaml")
		for schemeName, header := range want {
			scheme, ok := document.Components.SecuritySchemes[schemeName]
			if !ok {
				t.Errorf("%s is missing security scheme %s", name, schemeName)
				continue
			}
			if scheme.Type != "apiKey" || scheme.In != "header" || scheme.Name != header {
				t.Errorf("%s scheme %s = {%s %s %s}, want apiKey/header/%s", name, schemeName, scheme.Type, scheme.In, scheme.Name, header)
			}
		}
	}
}

func TestFixedPointTypesRemainStrings(t *testing.T) {
	for _, name := range []string{"trade", "perps"} {
		document := loadYAML[openAPIDocument](t, name+".yaml")
		for _, schemaName := range []string{"FixedPointDollars", "FixedPointCount"} {
			value, ok := document.Components.Schemas[schemaName]
			if !ok {
				t.Errorf("%s is missing %s", name, schemaName)
				continue
			}
			if value.Type != "string" {
				t.Errorf("%s %s type = %q, want string to prevent floating-point money", name, schemaName, value.Type)
			}
		}
	}
}

func TestCuratedMCPSurfaceAlignsWithUpstreamOperationsAndChannels(t *testing.T) {
	mcpSpec := loadYAML[manifest](t, "mcp-tools.yaml")
	if mcpSpec.Version != 1 {
		t.Fatalf("MCP manifest version = %d, want 1", mcpSpec.Version)
	}

	documents := map[string]*openAPIDocument{
		"trade": loadYAMLPointer[openAPIDocument](t, "trade.yaml"),
		"perps": loadYAMLPointer[openAPIDocument](t, "perps.yaml"),
	}
	operations := make(map[string]map[string]operationLocation)
	for name, document := range documents {
		operations[name] = indexOperations(document)
	}
	channels := map[string]map[string]any{
		"market_data_ws": loadYAML[asyncAPIDocument](t, "market_data_ws.yaml").Channels,
		"perps_ws":       loadYAML[asyncAPIDocument](t, "perps_ws.yaml").Channels,
	}

	wantTools := []string{"amend_order", "cancel_order", "get_market", "get_orderbook", "get_portfolio", "kernel_status", "place_order", "search_markets"}
	var gotTools []string
	seenHandlers := make(map[string]bool)
	for _, tool := range mcpSpec.Tools {
		gotTools = append(gotTools, tool.Name)
		if seenHandlers[tool.Handler] {
			t.Errorf("handler %s is reused", tool.Handler)
		}
		seenHandlers[tool.Handler] = true
		if tool.Safety == "trade" && !strings.Contains(strings.ToLower(tool.Description), "paper") {
			t.Errorf("mutating tool %s must explicitly describe paper behavior", tool.Name)
		}

		for _, source := range tool.Sources {
			if source.OperationID != "" {
				location, ok := operations[source.Spec][source.OperationID]
				if !ok {
					t.Errorf("tool %s references missing operation %s:%s", tool.Name, source.Spec, source.OperationID)
					continue
				}
				if tool.Safety == "read" && location.Method != "get" {
					t.Errorf("read tool %s references mutating %s %s", tool.Name, strings.ToUpper(location.Method), location.Path)
				}
				if tool.Safety == "trade" && location.Method == "get" {
					t.Errorf("trade tool %s references read-only %s %s", tool.Name, strings.ToUpper(location.Method), location.Path)
				}
				continue
			}
			if _, ok := channels[source.Spec][source.Channel]; !ok {
				t.Errorf("tool %s references missing channel %s:%s", tool.Name, source.Spec, source.Channel)
			}
		}
	}
	sort.Strings(gotTools)
	if strings.Join(gotTools, ",") != strings.Join(wantTools, ",") {
		t.Fatalf("curated tools = %v, want %v; review any surface-area change explicitly", gotTools, wantTools)
	}
}

func TestOrderToolRequiredFieldsAlignWithV2Requests(t *testing.T) {
	mcpSpec := loadYAML[manifest](t, "mcp-tools.yaml")
	documents := map[string]*openAPIDocument{
		"trade": loadYAMLPointer[openAPIDocument](t, "trade.yaml"),
		"perps": loadYAMLPointer[openAPIDocument](t, "perps.yaml"),
	}
	operations := map[string]map[string]operationLocation{
		"trade": indexOperations(documents["trade"]),
		"perps": indexOperations(documents["perps"]),
	}

	for _, toolName := range []string{"place_order", "amend_order"} {
		tool := findTool(t, mcpSpec, toolName)
		mcpRequired := make(map[string]bool)
		for _, property := range tool.Input.Properties {
			mcpRequired[property.Name] = property.Required
		}

		for _, source := range tool.Sources {
			location := operations[source.Spec][source.OperationID]
			request := resolveRequestSchema(t, location)
			for _, required := range request.Required {
				// The kernel creates this idempotency key before calling perps.
				if required == "client_order_id" {
					continue
				}
				if !mcpRequired[required] {
					t.Errorf("%s does not require upstream field %s required by %s:%s", toolName, required, source.Spec, source.OperationID)
				}
			}
		}
	}
}

func findTool(t *testing.T, spec manifest, name string) *struct {
	Name        string `yaml:"name"`
	Handler     string `yaml:"handler"`
	Safety      string `yaml:"safety"`
	Description string `yaml:"description"`
	Sources     []struct {
		Spec        string `yaml:"spec"`
		OperationID string `yaml:"operation_id"`
		Channel     string `yaml:"channel"`
	} `yaml:"sources"`
	Input struct {
		Properties []struct {
			Name     string   `yaml:"name"`
			Type     string   `yaml:"type"`
			Required bool     `yaml:"required"`
			Enum     []string `yaml:"enum"`
		} `yaml:"properties"`
	} `yaml:"input"`
} {
	t.Helper()
	for index := range spec.Tools {
		if spec.Tools[index].Name == name {
			return &spec.Tools[index]
		}
	}
	t.Fatalf("MCP tool %s not found", name)
	return nil
}

func resolveRequestSchema(t *testing.T, location operationLocation) schema {
	t.Helper()
	content, ok := location.Operation.RequestBody.Content["application/json"]
	if !ok {
		t.Fatalf("%s %s has no application/json request", strings.ToUpper(location.Method), location.Path)
	}
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(content.Schema.Ref, prefix) {
		t.Fatalf("%s %s request does not use a component schema", strings.ToUpper(location.Method), location.Path)
	}
	name := strings.TrimPrefix(content.Schema.Ref, prefix)
	request, ok := location.Document.Components.Schemas[name]
	if !ok {
		t.Fatalf("%s %s references missing schema %s", strings.ToUpper(location.Method), location.Path, name)
	}
	return request
}

func indexOperations(document *openAPIDocument) map[string]operationLocation {
	result := make(map[string]operationLocation)
	for path, pathItem := range document.Paths {
		for method, operation := range pathItem {
			method = strings.ToLower(method)
			if isHTTPMethod(method) && operation.OperationID != "" {
				result[operation.OperationID] = operationLocation{Method: method, Path: path, Operation: operation, Document: document}
			}
		}
	}
	return result
}

func isHTTPMethod(method string) bool {
	switch method {
	case "get", "put", "post", "delete", "patch", "head", "options", "trace":
		return true
	default:
		return false
	}
}

func sourceFilename(localFilename string) string {
	switch localFilename {
	case "trade.yaml":
		return "openapi.yaml"
	case "market_data_ws.yaml":
		return "asyncapi.yaml"
	case "perps.yaml":
		return "perps_openapi.yaml"
	case "perps_ws.yaml":
		return "perps_asyncapi.yaml"
	default:
		return ""
	}
}

func loadJSON[T any](t *testing.T, name string) T {
	t.Helper()
	contents := readSpecFile(t, name)
	var value T
	if err := json.Unmarshal(contents, &value); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return value
}

func loadYAML[T any](t *testing.T, name string) T {
	t.Helper()
	return *loadYAMLPointer[T](t, name)
}

func loadYAMLPointer[T any](t *testing.T, name string) *T {
	t.Helper()
	contents := readSpecFile(t, name)
	var value T
	if err := yaml.Unmarshal(contents, &value); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return &value
}

func readSpecFile(t *testing.T, name string) []byte {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "specs", name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return contents
}

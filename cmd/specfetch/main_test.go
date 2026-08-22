package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourcesArePinnedToOfficialHTTPSHost(t *testing.T) {
	seenFiles := make(map[string]bool)
	for _, item := range sources {
		parsed, err := url.Parse(item.URL)
		if err != nil {
			t.Fatalf("parse %s: %v", item.URL, err)
		}
		if err := validateSourceURL(parsed); err != nil {
			t.Errorf("source %s is not trusted: %v", item.Name, err)
		}
		if seenFiles[item.Filename] {
			t.Errorf("duplicate output filename %s", item.Filename)
		}
		seenFiles[item.Filename] = true
	}
	if len(sources) != 4 {
		t.Fatalf("source count = %d, want 4", len(sources))
	}
}

func TestValidateSourceURLRejectsAuthorityChanges(t *testing.T) {
	for _, rawURL := range []string{
		"http://docs.kalshi.com/openapi.yaml",
		"https://example.com/openapi.yaml",
		"https://docs.kalshi.com:8443/openapi.yaml",
		"https://user@docs.kalshi.com/openapi.yaml",
	} {
		t.Run(rawURL, func(t *testing.T) {
			parsed, err := url.Parse(rawURL)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateSourceURL(parsed); err == nil {
				t.Fatalf("accepted untrusted URL %s", rawURL)
			}
		})
	}
}

func TestValidateContractRequiresExpectedDialectAndSurface(t *testing.T) {
	validOpenAPI := []byte("openapi: 3.0.0\ninfo:\n  title: Trade\n  version: 1.2.3\npaths:\n  /status: {}\n")
	validAsyncAPI := []byte("asyncapi: 3.0.0\ninfo:\n  title: Stream\n  version: 2.0.0\nchannels:\n  ticker: {}\n")

	if _, err := validateContract(validOpenAPI, "openapi"); err != nil {
		t.Fatalf("valid OpenAPI rejected: %v", err)
	}
	if _, err := validateContract(validAsyncAPI, "asyncapi"); err != nil {
		t.Fatalf("valid AsyncAPI rejected: %v", err)
	}
	if _, err := validateContract(validOpenAPI, "asyncapi"); err == nil {
		t.Fatal("OpenAPI document accepted as AsyncAPI")
	}
	if _, err := validateContract([]byte("openapi: 3.0.0\ninfo: {title: Empty, version: 1}\npaths: {}\n"), "openapi"); err == nil {
		t.Fatal("empty OpenAPI surface accepted")
	}
}

func TestValidateContentType(t *testing.T) {
	for _, value := range []string{"text/yaml", "application/yaml; charset=utf-8", "application/octet-stream"} {
		if err := validateContentType(value); err != nil {
			t.Errorf("valid content type %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"text/html", "application/json", "", "not a media type"} {
		if err := validateContentType(value); err == nil {
			t.Errorf("invalid content type %q accepted", value)
		}
	}
}

func TestWriteSnapshotIncludesVerifiableProvenance(t *testing.T) {
	contents := []byte("openapi: 3.0.0\n")
	digest := sha256.Sum256(contents)
	result := fetchedSpec{
		Source:   source{Name: "trade", Filename: "trade.yaml", URL: "https://docs.kalshi.com/openapi.yaml", Dialect: "openapi"},
		Contents: contents, SHA256: hex.EncodeToString(digest[:]), Version: "3.28.0", Title: "Trade",
		ETag: `W/"example"`, LastModified: "Fri, 21 Aug 2026 22:07:08 GMT",
	}
	directory := t.TempDir()
	fetchedAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	if err := writeSnapshot(directory, []fetchedSpec{result}, fetchedAt); err != nil {
		t.Fatalf("writeSnapshot(): %v", err)
	}

	written, err := os.ReadFile(filepath.Join(directory, "trade.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(contents) {
		t.Fatal("snapshot bytes changed during write")
	}
	lock, err := os.ReadFile(filepath.Join(directory, "upstream.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded lockFile
	if err := json.Unmarshal(lock, &decoded); err != nil {
		t.Fatalf("decode lock: %v", err)
	}
	if decoded.FetchedAt != fetchedAt.Format(time.RFC3339) || decoded.SourceHost != trustedHost {
		t.Fatalf("unexpected lock provenance: %#v", decoded)
	}
	if len(decoded.Specs) != 1 {
		t.Fatalf("lock spec count = %d, want 1", len(decoded.Specs))
	}
	entry := decoded.Specs[0]
	if entry.SHA256 != result.SHA256 || entry.URL != result.Source.URL || entry.ETag != result.ETag {
		t.Fatalf("unexpected lock entry: %#v", entry)
	}
}

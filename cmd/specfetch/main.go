// Command specfetch downloads and validates the authoritative Kalshi API
// contracts. It only trusts HTTPS responses from docs.kalshi.com and writes no
// output until every source has passed validation.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	trustedHost = "docs.kalshi.com"
	maxSpecSize = 16 << 20
)

type source struct {
	Name     string
	URL      string
	Filename string
	Dialect  string
}

var sources = []source{
	{Name: "trade", URL: "https://docs.kalshi.com/openapi.yaml", Filename: "trade.yaml", Dialect: "openapi"},
	{Name: "market_data_ws", URL: "https://docs.kalshi.com/asyncapi.yaml", Filename: "market_data_ws.yaml", Dialect: "asyncapi"},
	{Name: "perps", URL: "https://docs.kalshi.com/perps_openapi.yaml", Filename: "perps.yaml", Dialect: "openapi"},
	{Name: "perps_ws", URL: "https://docs.kalshi.com/perps_asyncapi.yaml", Filename: "perps_ws.yaml", Dialect: "asyncapi"},
}

type fetchedSpec struct {
	Source       source
	Contents     []byte
	SHA256       string
	Version      string
	Title        string
	ETag         string
	LastModified string
}

type lockFile struct {
	SchemaVersion int         `json:"schema_version"`
	FetchedAt     string      `json:"fetched_at"`
	SourceHost    string      `json:"source_host"`
	Specs         []lockEntry `json:"specs"`
}

type lockEntry struct {
	Name         string `json:"name"`
	Filename     string `json:"filename"`
	URL          string `json:"url"`
	Dialect      string `json:"dialect"`
	Version      string `json:"version"`
	Title        string `json:"title"`
	SHA256       string `json:"sha256"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

type contractHeader struct {
	OpenAPI  string `yaml:"openapi"`
	AsyncAPI string `yaml:"asyncapi"`
	Info     struct {
		Title   string `yaml:"title"`
		Version string `yaml:"version"`
	} `yaml:"info"`
	Paths    map[string]any `yaml:"paths"`
	Channels map[string]any `yaml:"channels"`
}

func main() {
	outputDir := flag.String("output-dir", "", "directory in which validated specifications are written")
	timeout := flag.Duration("timeout", 60*time.Second, "total download timeout")
	flag.Parse()
	if *outputDir == "" {
		fatal(errors.New("-output-dir is required"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	client := newHTTPClient(*timeout)
	results, err := fetchAll(ctx, client)
	if err != nil {
		fatal(err)
	}
	if err := writeSnapshot(*outputDir, results, time.Now().UTC()); err != nil {
		fatal(err)
	}
	for _, result := range results {
		fmt.Printf("%s %s %s\n", result.Source.Name, result.Version, result.SHA256)
	}
}

func newHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return validateSourceURL(request.URL)
		},
	}
}

func fetchAll(ctx context.Context, client *http.Client) ([]fetchedSpec, error) {
	results := make([]fetchedSpec, len(sources))
	errorsByIndex := make([]error, len(sources))
	var wait sync.WaitGroup
	for index, item := range sources {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := fetchOne(ctx, client, item)
			results[index] = result
			errorsByIndex[index] = err
		}()
	}
	wait.Wait()
	for index, err := range errorsByIndex {
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", sources[index].Name, err)
		}
	}
	return results, nil
}

func fetchOne(ctx context.Context, client *http.Client, item source) (fetchedSpec, error) {
	parsedURL, err := url.Parse(item.URL)
	if err != nil {
		return fetchedSpec{}, err
	}
	if err := validateSourceURL(parsedURL); err != nil {
		return fetchedSpec{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return fetchedSpec{}, err
	}
	request.Header.Set("Accept", "application/yaml, text/yaml, application/octet-stream;q=0.5")
	request.Header.Set("User-Agent", "kalshi-kernel-specfetch/0.1")
	response, err := client.Do(request)
	if err != nil {
		return fetchedSpec{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fetchedSpec{}, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	if err := validateContentType(response.Header.Get("Content-Type")); err != nil {
		return fetchedSpec{}, err
	}

	contents, err := io.ReadAll(io.LimitReader(response.Body, maxSpecSize+1))
	if err != nil {
		return fetchedSpec{}, err
	}
	if len(contents) > maxSpecSize {
		return fetchedSpec{}, fmt.Errorf("response exceeds %d bytes", maxSpecSize)
	}
	header, err := validateContract(contents, item.Dialect)
	if err != nil {
		return fetchedSpec{}, err
	}
	digest := sha256.Sum256(contents)
	return fetchedSpec{
		Source:       item,
		Contents:     contents,
		SHA256:       hex.EncodeToString(digest[:]),
		Version:      header.Info.Version,
		Title:        header.Info.Title,
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
	}, nil
}

func validateSourceURL(value *url.URL) error {
	if value.Scheme != "https" {
		return fmt.Errorf("untrusted URL scheme %q", value.Scheme)
	}
	if !strings.EqualFold(value.Hostname(), trustedHost) {
		return fmt.Errorf("untrusted source host %q", value.Hostname())
	}
	if value.User != nil || value.Port() != "" {
		return errors.New("source URL may not contain user information or a custom port")
	}
	return nil
}

func validateContentType(value string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return fmt.Errorf("invalid Content-Type %q: %w", value, err)
	}
	switch mediaType {
	case "application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml", "application/octet-stream":
		return nil
	default:
		return fmt.Errorf("unexpected Content-Type %q", mediaType)
	}
}

func validateContract(contents []byte, dialect string) (contractHeader, error) {
	var header contractHeader
	if err := yaml.Unmarshal(contents, &header); err != nil {
		return contractHeader{}, fmt.Errorf("invalid YAML: %w", err)
	}
	if header.Info.Title == "" || header.Info.Version == "" {
		return contractHeader{}, errors.New("contract info.title and info.version are required")
	}
	switch dialect {
	case "openapi":
		if header.OpenAPI != "3.0.0" || len(header.Paths) == 0 {
			return contractHeader{}, fmt.Errorf("expected non-empty OpenAPI 3.0.0 contract, got %q", header.OpenAPI)
		}
	case "asyncapi":
		if header.AsyncAPI != "3.0.0" || len(header.Channels) == 0 {
			return contractHeader{}, fmt.Errorf("expected non-empty AsyncAPI 3.0.0 contract, got %q", header.AsyncAPI)
		}
	default:
		return contractHeader{}, fmt.Errorf("unsupported expected dialect %q", dialect)
	}
	return header, nil
}

func writeSnapshot(outputDir string, results []fetchedSpec, fetchedAt time.Time) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	lock := lockFile{SchemaVersion: 1, FetchedAt: fetchedAt.Format(time.RFC3339), SourceHost: trustedHost}
	for _, result := range results {
		if err := atomicWrite(filepath.Join(outputDir, result.Source.Filename), result.Contents); err != nil {
			return err
		}
		lock.Specs = append(lock.Specs, lockEntry{
			Name: result.Source.Name, Filename: result.Source.Filename, URL: result.Source.URL,
			Dialect: result.Source.Dialect, Version: result.Version, Title: result.Title,
			SHA256: result.SHA256, ETag: result.ETag, LastModified: result.LastModified,
		})
	}
	sort.Slice(lock.Specs, func(left, right int) bool { return lock.Specs[left].Name < lock.Specs[right].Name })
	encoded, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return atomicWrite(filepath.Join(outputDir, "upstream.lock.json"), encoded)
}

func atomicWrite(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".specfetch-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "specfetch:", err)
	os.Exit(1)
}

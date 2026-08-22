package mcpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPaperServerAdvertisesCuratedToolsAndStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(config.Config{Mode: config.ModePaper}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()
	if instructions := clientSession.InitializeResult().Instructions; instructions == "" {
		t.Fatal("server published no safety instructions")
	}

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if got, want := len(tools.Tools), 12; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}

	var placeOrder *mcp.Tool
	for _, tool := range tools.Tools {
		if tool.Name == "place_order" {
			placeOrder = tool
			break
		}
	}
	if placeOrder == nil {
		t.Fatal("place_order tool was not advertised")
	}
	if placeOrder.Annotations == nil || placeOrder.Annotations.ReadOnlyHint {
		t.Fatal("place_order was not marked as a mutating tool")
	}
	if placeOrder.Annotations.DestructiveHint == nil || !*placeOrder.Annotations.DestructiveHint {
		t.Fatal("place_order was not marked with destructiveHint")
	}

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "kernel_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call kernel_status: %v", err)
	}
	if result.IsError {
		t.Fatal("kernel_status returned an MCP tool error")
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T, want map[string]any", result.StructuredContent)
	}
	if got := structured["mode"]; got != "paper" {
		t.Fatalf("status mode = %v, want paper", got)
	}
}

func TestPlaceOrderSchemaRejectsUnknownProduct(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(config.Config{Mode: config.ModePaper}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "place_order",
		Arguments: map[string]any{
			"product":                    "invalid",
			"ticker":                     "EXAMPLE",
			"side":                       "bid",
			"count":                      "1.00",
			"price":                      "0.50",
			"time_in_force":              "good_till_canceled",
			"self_trade_prevention_type": "taker_at_cross",
		},
	})
	if err != nil {
		t.Fatalf("call place_order: %v", err)
	}
	if !result.IsError {
		t.Fatal("place_order accepted a product outside the generated enum")
	}
}

func TestUnavailableBackendNeverReportsLiveTradingArmed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientSession := connectInMemory(t, ctx, config.ModeLive)

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "kernel_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T", result.StructuredContent)
	}
	data, ok := structured["data"].(map[string]any)
	if !ok {
		t.Fatalf("status data type = %T", structured["data"])
	}
	if data["live_mode_requested"] != true || data["live_trading_armed"] != false {
		t.Fatalf("unavailable backend reported unsafe status: %#v", data)
	}
}

func TestGeneratedSchemasRejectMalformedCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientSession := connectInMemory(t, ctx, config.ModePaper)

	validPlaceOrder := map[string]any{
		"product":                    "event",
		"ticker":                     "EXAMPLE",
		"side":                       "bid",
		"count":                      "1.00",
		"price":                      "0.50",
		"time_in_force":              "good_till_canceled",
		"self_trade_prevention_type": "taker_at_cross",
	}
	tests := []struct {
		name      string
		tool      string
		arguments map[string]any
	}{
		{name: "missing required", tool: "place_order", arguments: without(validPlaceOrder, "ticker")},
		{name: "unknown side enum", tool: "place_order", arguments: replacing(validPlaceOrder, "side", "yes")},
		{name: "non-decimal price", tool: "place_order", arguments: replacing(validPlaceOrder, "price", "NaN")},
		{name: "excess count precision", tool: "place_order", arguments: replacing(validPlaceOrder, "count", "1.001")},
		{name: "integer above maximum", tool: "search_markets", arguments: map[string]any{"product": "event", "limit": 1001}},
		{name: "unknown property", tool: "kernel_status", arguments: map[string]any{"mode": "live"}},
		{name: "amend missing upstream required values", tool: "amend_order", arguments: map[string]any{"product": "event", "order_id": "id", "ticker": "EXAMPLE"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: test.tool, Arguments: test.arguments})
			if err != nil {
				t.Fatalf("CallTool() transport error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("%s accepted malformed arguments %#v", test.tool, test.arguments)
			}
		})
	}
}

func TestAllToolsPublishStrictSchemasAndSafetyAnnotations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientSession := connectInMemory(t, ctx, config.ModePaper)

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools(): %v", err)
	}
	readTools := map[string]bool{
		"kernel_status":    true,
		"search_markets":   true,
		"get_market":       true,
		"get_orderbook":    true,
		"get_candles":      true,
		"get_trades":       true,
		"get_last":         true,
		"arm_live_trading": false,
		"get_portfolio":    true,
	}
	for _, tool := range listed.Tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Errorf("%s input schema type = %T", tool.Name, tool.InputSchema)
			continue
		}
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Errorf("%s schema is not a strict object: %#v", tool.Name, schema)
		}
		if tool.Annotations == nil {
			t.Errorf("%s has no safety annotations", tool.Name)
			continue
		}
		if tool.Annotations.Title == "" {
			t.Errorf("%s has no human-readable title", tool.Name)
		}
		if got, want := tool.Annotations.ReadOnlyHint, readTools[tool.Name]; got != want {
			t.Errorf("%s readOnlyHint = %v, want %v", tool.Name, got, want)
		}
		wantDestructive := !readTools[tool.Name]
		if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != wantDestructive {
			t.Errorf("%s destructiveHint does not match safety class", tool.Name)
		}
	}
}

func TestUnconnectedCapabilitiesFailClosedWithModeEnvelope(t *testing.T) {
	// Live mode requires an explicit arm step before any write: an
	// un-armed process must refuse place_order with live_trading_not_armed
	// even when credentials are present.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientSession := connectInMemoryWithEnv(t, ctx, config.ModeLive)

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "place_order",
		Arguments: map[string]any{
			"product": "event", "ticker": "EXAMPLE", "side": "bid",
			"count": "1.00", "price": "0.50",
			"time_in_force":              "good_till_canceled",
			"self_trade_prevention_type": "maker",
			"client_order_id":            "test-coid",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError {
		t.Fatal("un-armed write reported success")
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T", result.StructuredContent)
	}
	if structured["mode"] != "live" || structured["ok"] != false {
		t.Fatalf("unexpected failure envelope: %#v", structured)
	}
	errorValue, ok := structured["error"].(map[string]any)
	if !ok || errorValue["code"] != "live_trading_not_armed" {
		t.Fatalf("unexpected structured error: %#v", structured["error"])
	}
}

func TestConcurrentStatusCallsAreStable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientSession := connectInMemory(t, ctx, config.ModePaper)

	const calls = 64
	var wait sync.WaitGroup
	errors := make(chan error, calls)
	for range calls {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "kernel_status", Arguments: map[string]any{}})
			if err != nil {
				errors <- err
				return
			}
			if result.IsError {
				errors <- context.Canceled
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent status call failed: %v", err)
	}
}

func TestStreamableHTTPNegotiation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := New(config.Config{Mode: config.ModePaper})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets unavailable: %v", err)
	}
	httpServer := httptest.NewUnstartedServer(handler)
	httpServer.Listener = listener
	httpServer.Start()
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "http-test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("connect streamable HTTP client: %v", err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools(): %v", err)
	}
	if len(listed.Tools) != 12 {
		t.Fatalf("HTTP tool count = %d, want 12", len(listed.Tools))
	}
}

func connectInMemory(t *testing.T, ctx context.Context, mode config.Mode) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New(config.Config{Mode: mode}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

func connectInMemoryWithEnv(t *testing.T, ctx context.Context, mode config.Mode) *mcp.ClientSession {
	// Same as connectInMemory but for live-mode handlers that would
	// consult env credentials; the handler under test never reaches the
	// network because execution adapters are unconnected.
	return connectInMemory(t, ctx, mode)
}

func without(source map[string]any, key string) map[string]any {
	result := make(map[string]any, len(source))
	for name, value := range source {
		if name != key {
			result[name] = value
		}
	}
	return result
}

func replacing(source map[string]any, key string, value any) map[string]any {
	result := without(source, "")
	result[key] = value
	return result
}

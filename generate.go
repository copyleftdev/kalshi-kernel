package kalshikernel

//go:generate go tool oapi-codegen --config config/oapi/trade.yaml specs/trade.yaml
//go:generate go tool oapi-codegen --config config/oapi/perps.yaml specs/perps.yaml
//go:generate go run ./cmd/mcpgen -spec specs/mcp-tools.yaml -out internal/gen/mcptools/tools.gen.go

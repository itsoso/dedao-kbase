package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/yann0917/dedao-gui/backend/app"
)

const mcpProtocolVersion = "2025-03-26"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *rpcErrorBody `json:"error,omitempty"`
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

func main() {
	root := flag.String("root", app.DefaultBookKnowledgeRoot(), "book_knowledge root directory")
	flag.Parse()

	store := app.NewBookKnowledgeStore(*root)
	server := app.NewBookKnowledgeMCPServer(store)
	if err := serveStdio(os.Stdin, os.Stdout, server); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serveStdio(input io.Reader, output io.Writer, server *app.BookKnowledgeMCPServer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			if err := encoder.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcErrorBody{Code: -32700, Message: err.Error()},
			}); err != nil {
				return err
			}
			continue
		}
		if isNotification(request) {
			continue
		}
		response := handleRequest(request, server)
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handleRequest(request rpcRequest, server *app.BookKnowledgeMCPServer) rpcResponse {
	response := rpcResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		var params initializeParams
		_ = json.Unmarshal(request.Params, &params)
		version := params.ProtocolVersion
		if version == "" {
			version = mcpProtocolVersion
		}
		response.Result = map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    "dedao-book-knowledge",
				"version": "0.1.0",
			},
		}
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		response.Result = map[string]any{
			"tools": server.Tools(),
		}
	case "tools/call":
		result, err := callTool(request.Params, server)
		if err != nil {
			response.Error = &rpcErrorBody{Code: -32602, Message: err.Error()}
			return response
		}
		response.Result = result
	default:
		response.Error = &rpcErrorBody{Code: -32601, Message: "method not found: " + request.Method}
	}
	return response
}

func isNotification(request rpcRequest) bool {
	return request.ID == nil && request.Method != ""
}

func callTool(params json.RawMessage, server *app.BookKnowledgeMCPServer) (map[string]any, error) {
	var input callToolParams
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, err
	}
	result, err := server.Call(input.Name, input.Arguments)
	if err != nil {
		return nil, err
	}
	var structured any
	if err := json.Unmarshal(result, &structured); err != nil {
		structured = string(result)
	}
	return map[string]any{
		"resultType":        "complete",
		"content":           []map[string]string{{"type": "text", "text": string(result)}},
		"structuredContent": structured,
		"isError":           false,
	}, nil
}

package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	kbaseSkillSearch           = "dedao.book.search"
	kbaseSkillGetContext       = "dedao.book.get_context"
	kbaseSkillSystemKBManifest = "dedao.system_kb.manifest"
	kbaseSkillSystemKBExport   = "dedao.system_kb.export"
)

type kbaseSkillDefinition struct {
	Name        string
	Title       string
	Description string
	InputSchema map[string]any
}

func kbaseSkillDefinitions() []kbaseSkillDefinition {
	return []kbaseSkillDefinition{
		{
			Name:        kbaseSkillSearch,
			Title:       "Search Dedao book knowledge",
			Description: "Search extracted Dedao book chunks and claims. Results include stable book_id, chunk_id, claim_id, snippets, and scores for citation-aware downstream use.",
			InputSchema: objectSchema(map[string]any{
				"query":   map[string]any{"type": "string", "description": "Search query."},
				"book_id": map[string]any{"type": "string", "description": "Optional book_id filter."},
				"limit":   map[string]any{"type": "integer", "description": "Maximum result count."},
			}),
		},
		{
			Name:        kbaseSkillGetContext,
			Title:       "Get Dedao book context",
			Description: "Read one extracted book package by book_id. Use search first, then call this for focused context when a downstream system needs chapters, chunks, claims, and citations.",
			InputSchema: objectSchema(map[string]any{
				"book_id":    map[string]any{"type": "string", "description": "Required book_id."},
				"max_chunks": map[string]any{"type": "integer", "description": "Optional chunk cap. Zero means no cap."},
				"max_claims": map[string]any{"type": "integer", "description": "Optional claim cap. Zero means no cap."},
			}),
		},
		{
			Name:        kbaseSkillSystemKBManifest,
			Title:       "Get Dedao System KB manifest",
			Description: "Read public metadata for the generated System KB export so importers can compare versions, source, timestamps, and stats before pulling the full payload.",
			InputSchema: objectSchema(map[string]any{}),
		},
		{
			Name:        kbaseSkillSystemKBExport,
			Title:       "Get Dedao System KB export",
			Description: "Read the generated System KB export payload for governed import into health, proofroom, or other downstream systems.",
			InputSchema: objectSchema(map[string]any{}),
		},
	}
}

func (h *kbaseHTTPHandler) handleSkillsDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"service":     "dedao-kbase",
		"description": "Private Dedao book knowledge skills. Discovery is public; invocation requires bearer auth.",
		"auth": map[string]any{
			"type":       "bearer",
			"header":     "Authorization",
			"scheme":     "Bearer",
			"env":        "DEDAO_KBASE_TOKEN",
			"configured": h.authToken != "",
		},
		"skills": h.skillSummaries(),
	})
}

func (h *kbaseHTTPHandler) handleSkillRoute(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/skills" {
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"skills": h.skillSummaries()})
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/skills/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		writeHTTPError(w, http.StatusNotFound, "not found")
		return
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid skill name")
		return
	}
	def, ok := findKBaseSkillDefinition(name)
	if !ok {
		writeHTTPError(w, http.StatusNotFound, "skill not found")
		return
	}

	switch parts[1] {
	case "manifest.json":
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeHTTPJSON(w, http.StatusOK, h.skillManifest(def))
	case "openapi.json":
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeHTTPJSON(w, http.StatusOK, h.skillOpenAPI(def))
	case "SKILL.md":
		if r.Method != http.MethodGet {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(h.skillMarkdown(def)))
	case "invoke":
		if r.Method != http.MethodPost {
			writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !h.authorize(w, r) {
			return
		}
		h.invokeSkill(w, r, def)
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (h *kbaseHTTPHandler) invokeSkill(w http.ResponseWriter, r *http.Request, def kbaseSkillDefinition) {
	arguments, err := readSkillArguments(r)
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, err.Error())
		return
	}

	var result any
	switch def.Name {
	case kbaseSkillSearch:
		var query BookKnowledgeSearchQuery
		if len(arguments) > 0 {
			if err := json.Unmarshal(arguments, &query); err != nil {
				writeHTTPError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		results, err := h.store.Search(query)
		if err != nil {
			writeHTTPError(w, http.StatusInternalServerError, err.Error())
			return
		}
		result = map[string]any{"results": results}
	case kbaseSkillGetContext:
		var input struct {
			BookID    string `json:"book_id"`
			MaxChunks int    `json:"max_chunks"`
			MaxClaims int    `json:"max_claims"`
		}
		if err := json.Unmarshal(arguments, &input); err != nil {
			writeHTTPError(w, http.StatusBadRequest, err.Error())
			return
		}
		pkg, err := h.store.LoadPackage(input.BookID)
		if err != nil {
			writeHTTPError(w, http.StatusNotFound, err.Error())
			return
		}
		result = limitedBookKnowledgePackage(pkg, input.MaxChunks, input.MaxClaims)
	case kbaseSkillSystemKBManifest:
		manifest, status, err := h.systemKBManifest()
		if err != nil {
			writeHTTPError(w, status, err.Error())
			return
		}
		result = manifest
	case kbaseSkillSystemKBExport:
		payload, err := h.readSystemKBExport()
		if err != nil {
			writeHTTPError(w, http.StatusNotFound, err.Error())
			return
		}
		var decoded any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			writeHTTPError(w, http.StatusInternalServerError, fmt.Sprintf("invalid system kb export: %v", err))
			return
		}
		result = decoded
	default:
		writeHTTPError(w, http.StatusNotFound, "skill not found")
		return
	}

	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"skill":  def.Name,
		"status": "ok",
		"result": result,
	})
}

func readSkillArguments(r *http.Request) (json.RawMessage, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	if args, ok := envelope["arguments"]; ok {
		return args, nil
	}
	return json.RawMessage(body), nil
}

func limitedBookKnowledgePackage(pkg *BookKnowledgePackage, maxChunks, maxClaims int) map[string]any {
	chunks := pkg.Chunks
	claims := pkg.Claims
	truncated := map[string]bool{"chunks": false, "claims": false}
	if maxChunks > 0 && len(chunks) > maxChunks {
		chunks = chunks[:maxChunks]
		truncated["chunks"] = true
	}
	if maxClaims > 0 && len(claims) > maxClaims {
		claims = claims[:maxClaims]
		truncated["claims"] = true
	}
	return map[string]any{
		"book":      pkg.Book,
		"chapters":  pkg.Chapters,
		"chunks":    chunks,
		"claims":    claims,
		"citations": pkg.Citations,
		"truncated": truncated,
	}
}

func (h *kbaseHTTPHandler) skillSummaries() []map[string]any {
	defs := kbaseSkillDefinitions()
	summaries := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		summaries = append(summaries, map[string]any{
			"name":         def.Name,
			"title":        def.Title,
			"description":  def.Description,
			"manifest_url": skillPath(def.Name, "manifest.json"),
			"openapi_url":  skillPath(def.Name, "openapi.json"),
			"skill_url":    skillPath(def.Name, "SKILL.md"),
			"invoke_url":   skillPath(def.Name, "invoke"),
			"auth":         map[string]any{"type": "bearer", "required_for": []string{"invoke"}},
		})
	}
	return summaries
}

func (h *kbaseHTTPHandler) skillManifest(def kbaseSkillDefinition) map[string]any {
	return map[string]any{
		"name":         def.Name,
		"title":        def.Title,
		"description":  def.Description,
		"version":      "0.1.0",
		"input_schema": def.InputSchema,
		"auth": map[string]any{
			"type":   "bearer",
			"header": "Authorization",
			"scheme": "Bearer",
			"env":    "DEDAO_KBASE_TOKEN",
		},
		"openapi_url": skillPath(def.Name, "openapi.json"),
		"skill_url":   skillPath(def.Name, "SKILL.md"),
		"invoke_url":  skillPath(def.Name, "invoke"),
	}
}

func (h *kbaseHTTPHandler) skillOpenAPI(def kbaseSkillDefinition) map[string]any {
	invokePath := skillPath(def.Name, "invoke")
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   def.Title,
			"version": "0.1.0",
		},
		"paths": map[string]any{
			invokePath: map[string]any{
				"post": map[string]any{
					"summary":     def.Description,
					"operationId": strings.ReplaceAll(def.Name, ".", "_"),
					"security":    []map[string]any{{"bearerAuth": []any{}}},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": objectSchema(map[string]any{
									"arguments": def.InputSchema,
								}),
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Skill invocation result."},
						"401": map[string]any{"description": "Bearer token is missing or invalid."},
					},
				},
			},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "KBASE_AUTH_TOKEN",
				},
			},
		},
	}
}

func (h *kbaseHTTPHandler) skillMarkdown(def kbaseSkillDefinition) string {
	return fmt.Sprintf(`# %s

%s

## Authentication

Invocation requires a preconfigured bearer token. Callers should read it from `+"`DEDAO_KBASE_TOKEN`"+` and send:

`+"```http"+`
Authorization: Bearer ${DEDAO_KBASE_TOKEN}
`+"```"+`

Discovery documents are public. Do not put the token in the public manifest, OpenAPI file, or skill text.

## Invoke

Endpoint:

`+"```text"+`
POST %s
`+"```"+`

Request body:

`+"```json"+`
{"arguments": {}}
`+"```"+`

Use this skill for private Dedao book knowledge retrieval. Treat returned claims as draft source material unless a downstream system has explicitly reviewed and promoted them.
`, def.Name, def.Description, skillPath(def.Name, "invoke"))
}

func findKBaseSkillDefinition(name string) (kbaseSkillDefinition, bool) {
	for _, def := range kbaseSkillDefinitions() {
		if def.Name == name {
			return def, true
		}
	}
	return kbaseSkillDefinition{}, false
}

func skillPath(name, suffix string) string {
	return "/api/skills/" + url.PathEscape(name) + "/" + suffix
}

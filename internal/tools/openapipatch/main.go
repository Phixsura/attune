// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const openAPIPath = "docs/openapi/openapi.yaml"

const (
	apiVersionHeaderName    = "X-Attune-Api-Version"
	apiDeprecationHeader    = "Deprecation"
	apiSunsetHeader         = "Sunset"
	currentPublicAPIVersion = "2026-06-28"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "openapipatch: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	raw, err := os.ReadFile(openAPIPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", openAPIPath, err)
	}
	patched, err := PatchOpenAPI(raw)
	if err != nil {
		return err
	}
	if bytes.Equal(raw, patched) {
		return nil
	}
	if err := os.WriteFile(openAPIPath, patched, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", openAPIPath, err)
	}
	return nil
}

// PatchOpenAPI repairs generator gaps in the committed OpenAPI document while
// keeping proto as the single source of truth for paths and typed payloads.
func PatchOpenAPI(raw []byte) ([]byte, error) {
	doc := ptrext.Of(yaml.Node{})
	if err := yaml.Unmarshal(raw, doc); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", filepath.Base(openAPIPath), err)
	}

	if err := patchDocument(doc); err != nil {
		return nil, err
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", filepath.Base(openAPIPath), err)
	}
	return out, nil
}

func patchDocument(doc *yaml.Node) error {
	root, err := documentMapping(doc)
	if err != nil {
		return err
	}

	stripComments(doc)

	components := ensureMapping(root, "components")
	schemas := ensureMapping(components, "schemas")
	ensureErrorResponseSchema(schemas)
	sanitizeStatusSchema(schemas)

	paths := ensureMapping(root, "paths")
	patchPaths(paths)

	return nil
}

// Strip generator comments so the committed OpenAPI artifact does not carry
// external URLs that trufflehog flags as unknown findings.
func stripComments(node *yaml.Node) {
	if node == nil {
		return
	}
	node.HeadComment = ""
	node.LineComment = ""
	node.FootComment = ""
	for _, child := range node.Content {
		stripComments(child)
	}
}

func documentMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, errors.New("expected one-document YAML")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("expected root mapping")
	}
	return root, nil
}

func patchPaths(paths *yaml.Node) {
	for i := 0; i < len(paths.Content); i += 2 {
		path := paths.Content[i].Value
		pathItem := paths.Content[i+1]
		if pathItem.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j < len(pathItem.Content); j += 2 {
			method := pathItem.Content[j].Value
			if !isHTTPOperation(method) {
				continue
			}
			op := pathItem.Content[j+1]
			if op.Kind != yaml.MappingNode {
				continue
			}
			responses := mappingValue(op, "responses")
			if responses == nil || responses.Kind != yaml.MappingNode {
				continue
			}
			patchResponses(responses)
			if isVersionedPublicPath(path) {
				ensureAPIVersionParameter(op)
				patchVersionHeaders(responses)
			}
		}
	}
}

func isVersionedPublicPath(path string) bool {
	return strings.HasPrefix(path, "/v1/") && !strings.HasPrefix(path, "/v1/inbound/")
}

func isHTTPOperation(method string) bool {
	switch strings.ToLower(method) {
	case "get", "post", "put", "patch", "delete", "options", "head", "trace":
		return true
	default:
		return false
	}
}

func patchResponses(responses *yaml.Node) {
	for i := 0; i < len(responses.Content); i += 2 {
		code := responses.Content[i].Value
		resp := responses.Content[i+1]
		if resp.Kind != yaml.MappingNode {
			continue
		}
		if code == "default" || isErrorStatus(code) {
			ensureJSONSchemaRef(resp, "#/components/schemas/ErrorResponse")
		}
	}
}

func patchVersionHeaders(responses *yaml.Node) {
	for i := 0; i < len(responses.Content); i += 2 {
		resp := responses.Content[i+1]
		if resp.Kind != yaml.MappingNode {
			continue
		}
		headers := ensureMapping(resp, "headers")
		ensureResponseHeader(headers, apiVersionHeaderName,
			"Effective attune API version used to process the request. Echoes the caller-supplied X-Attune-Api-Version value or the server default when omitted.",
			currentPublicAPIVersion,
		)
		ensureResponseHeader(headers, apiDeprecationHeader,
			"Present when the selected API version is deprecated but still supported. Value follows the HTTP Deprecation header syntax.",
			"",
		)
		ensureResponseHeader(headers, apiSunsetHeader,
			"Present when the selected API version has a scheduled removal date. Value is an HTTP-date.",
			"",
		)
	}
}

func isErrorStatus(code string) bool {
	return len(code) == 3 && (code[0] == '4' || code[0] == '5')
}

func ensureJSONSchemaRef(response *yaml.Node, ref string) {
	content := ensureMapping(response, "content")
	appJSON := ensureMapping(content, "application/json")
	schema := ensureMapping(appJSON, "schema")
	setScalar(schema, "$ref", ref)
}

func ensureAPIVersionParameter(op *yaml.Node) {
	parameters := ensureSequence(op, "parameters")
	param := findHeaderParameter(parameters, apiVersionHeaderName)
	if param == nil {
		param = mappingNode()
		parameters.Content = append(parameters.Content, param)
	}
	setScalar(param, "name", apiVersionHeaderName)
	setScalar(param, "in", "header")
	setBool(param, "required", false)
	setScalar(param, "description", "Optional request header that pins attune's date-based public API contract. Omit it to use the server's current default. Unsupported values return the shared ErrorResponse envelope with BAD_REQUEST.")
	schema := ensureMapping(param, "schema")
	setScalar(schema, "type", "string")
	setScalar(schema, "example", currentPublicAPIVersion)
}

func findHeaderParameter(parameters *yaml.Node, name string) *yaml.Node {
	if parameters == nil || parameters.Kind != yaml.SequenceNode {
		return nil
	}
	for _, item := range parameters.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if gotName := mappingValue(item, "name"); gotName != nil && gotName.Value == name {
			if gotIn := mappingValue(item, "in"); gotIn != nil && gotIn.Value == "header" {
				return item
			}
		}
	}
	return nil
}

func ensureResponseHeader(headers *yaml.Node, name, description, example string) {
	header := ensureMapping(headers, name)
	setScalar(header, "description", description)
	schema := ensureMapping(header, "schema")
	setScalar(schema, "type", "string")
	if example != "" {
		setScalar(schema, "example", example)
		return
	}
	deleteMappingKey(schema, "example")
}

func ensureErrorResponseSchema(schemas *yaml.Node) {
	if got := mappingValue(schemas, "ErrorResponse"); got != nil {
		replacement := errorResponseSchema()
		got.Kind = replacement.Kind
		got.Style = replacement.Style
		got.Tag = replacement.Tag
		got.Value = replacement.Value
		got.Anchor = replacement.Anchor
		got.Alias = replacement.Alias
		got.Content = replacement.Content
		got.HeadComment = replacement.HeadComment
		got.LineComment = replacement.LineComment
		got.FootComment = replacement.FootComment
		got.Line = replacement.Line
		got.Column = replacement.Column
		return
	}
	appendMappingPair(schemas, "ErrorResponse", errorResponseSchema())
}

func sanitizeStatusSchema(schemas *yaml.Node) {
	status := mappingValue(schemas, "Status")
	if status == nil || status.Kind != yaml.MappingNode {
		return
	}
	setScalar(status, "description", "The `Status` type defines a logical error model suitable for REST APIs and RPC APIs. It contains error code, error message, and error details.")
}

func errorResponseSchema() *yaml.Node {
	return mappingNode(
		"required", sequenceNode(
			ptrext.Indirect(scalarNode("code")),
			ptrext.Indirect(scalarNode("message")),
		),
		"type", scalarNode("object"),
		"properties", mappingNode(
			"code", mappingNode(
				"type", scalarNode("string"),
				"description", scalarNode("Stable machine-readable error code. Values are the attune ErrorCode enum names, such as VALIDATION or RATE_LIMITED."),
			),
			"message", mappingNode(
				"type", scalarNode("string"),
				"description", scalarNode("Human-facing error message. Not stable; do not switch on it."),
			),
			"requestId", mappingNode(
				"type", scalarNode("string"),
				"description", scalarNode("Request id for support and log correlation. Mirrors the HTTP request-id/trace identifier when available."),
			),
		),
		"description", scalarNode("attune-standard error envelope for all non-2xx HTTP responses."),
	)
}

func ensureMapping(parent *yaml.Node, key string) *yaml.Node {
	if got := mappingValue(parent, key); got != nil {
		if got.Kind == 0 {
			got.Kind = yaml.MappingNode
			got.Tag = "!!map"
		}
		return got
	}
	child := ptrext.Of(yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	appendMappingPair(parent, key, child)
	return child
}

func ensureSequence(parent *yaml.Node, key string) *yaml.Node {
	if got := mappingValue(parent, key); got != nil {
		if got.Kind == 0 {
			got.Kind = yaml.SequenceNode
			got.Tag = "!!seq"
		}
		return got
	}
	child := ptrext.Of(yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"})
	appendMappingPair(parent, key, child)
	return child
}

func setScalar(parent *yaml.Node, key, value string) {
	if got := mappingValue(parent, key); got != nil {
		got.Kind = yaml.ScalarNode
		got.Tag = "!!str"
		got.Value = value
		return
	}
	appendMappingPair(parent, key, scalarNode(value))
}

func setBool(parent *yaml.Node, key string, value bool) {
	if got := mappingValue(parent, key); got != nil {
		if value {
			got.Kind = yaml.ScalarNode
			got.Tag = "!!bool"
			got.Value = "true"
			return
		}
		got.Kind = yaml.ScalarNode
		got.Tag = "!!bool"
		got.Value = "false"
		return
	}
	appendMappingPair(parent, key, boolNode(value))
}

func deleteMappingKey(parent *yaml.Node, key string) {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value != key {
			continue
		}
		parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
		return
	}
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func appendMappingPair(parent *yaml.Node, key string, value *yaml.Node) {
	parent.Content = append(parent.Content, scalarNode(key), value)
}

func mappingNode(kv ...any) *yaml.Node {
	node := ptrext.Of(yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
	for i := 0; i < len(kv); i += 2 {
		key := kv[i].(string)
		val := kv[i+1].(*yaml.Node)
		node.Content = append(node.Content, scalarNode(key), val)
	}
	return node
}

func sequenceNode(items ...yaml.Node) *yaml.Node {
	content := make([]*yaml.Node, 0, len(items))
	for _, item := range items {
		content = append(content, ptrext.Of(item))
	}
	return ptrext.Of(yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: content})
}

func scalarNode(value string) *yaml.Node {
	return ptrext.Of(yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func boolNode(value bool) *yaml.Node {
	if value {
		return ptrext.Of(yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	return ptrext.Of(yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"})
}

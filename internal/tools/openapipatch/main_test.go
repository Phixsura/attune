// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestPatchOpenAPI_AddsAttuneErrorContract(t *testing.T) {
	t.Parallel()

	in := []byte(`openapi: 3.0.3
paths:
  /v1/feedback/ingest:
    post:
      responses:
        "200":
          description: OK
        default:
          description: Default error response
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Status'
        "409":
          description: conflict
components:
  schemas:
    Status:
      type: object
`)

	out, err := PatchOpenAPI(in)
	if err != nil {
		t.Fatalf("PatchOpenAPI: %v", err)
	}

	root := parseYAMLDoc(t, out)
	schemas := mappingValue(mappingValue(mappingValue(root, "components"), "schemas"), "ErrorResponse")
	if schemas == nil {
		t.Fatal("ErrorResponse schema missing")
	}
	if got := mappingValue(schemas, "description"); got == nil || got.Value == "" {
		t.Fatal("ErrorResponse description missing")
	}

	responses := mappingValue(mappingValue(mappingValue(mappingValue(root, "paths"), "/v1/feedback/ingest"), "post"), "responses")
	parameters := mappingValue(mappingValue(mappingValue(root, "paths"), "/v1/feedback/ingest"), "post")
	assertSchemaRef(t, mappingValue(responses, "default"), "#/components/schemas/ErrorResponse")
	assertSchemaRef(t, mappingValue(responses, "409"), "#/components/schemas/ErrorResponse")
	assertHeaderParameter(t, parameters, apiVersionHeaderName)
	assertResponseHeader(t, mappingValue(responses, "200"), apiVersionHeaderName)
	assertResponseHeader(t, mappingValue(responses, "200"), apiDeprecationHeader)
	assertResponseHeader(t, mappingValue(responses, "200"), apiSunsetHeader)
}

func TestPatchOpenAPI_IsIdempotent(t *testing.T) {
	t.Parallel()

	in := []byte(`openapi: 3.0.3
paths:
  /v1/test:
    get:
      responses:
        default:
          description: Default error response
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ErrorResponse'
components:
  schemas:
    ErrorResponse:
      required:
        - code
        - message
      type: object
      properties:
        code:
          type: string
        message:
          type: string
        requestId:
          type: string
`)

	once, err := PatchOpenAPI(in)
	if err != nil {
		t.Fatalf("PatchOpenAPI(first): %v", err)
	}
	twice, err := PatchOpenAPI(once)
	if err != nil {
		t.Fatalf("PatchOpenAPI(second): %v", err)
	}

	if string(once) != string(twice) {
		t.Fatal("PatchOpenAPI is not idempotent")
	}
}

func TestPatchOpenAPI_RepairsStaleExistingContractNodes(t *testing.T) {
	t.Parallel()

	in := []byte(`openapi: 3.0.3
paths:
  /v1/test:
    get:
      parameters:
        - name: X-Attune-Api-Version
          in: header
          required: true
          description: stale
          schema:
            type: integer
      responses:
        "200":
          description: OK
          headers:
            Deprecation:
              description: stale deprecation
              schema:
                type: string
                example: stale
components:
  schemas:
    ErrorResponse:
      type: string
`)

	out, err := PatchOpenAPI(in)
	if err != nil {
		t.Fatalf("PatchOpenAPI: %v", err)
	}

	root := parseYAMLDoc(t, out)
	op := mappingValue(mappingValue(mappingValue(root, "paths"), "/v1/test"), "get")
	assertHeaderParameter(t, op, apiVersionHeaderName)
	parameters := mappingValue(op, "parameters")
	param := findHeaderParameter(parameters, apiVersionHeaderName)
	if got := mappingValue(param, "required"); got == nil || got.Tag != "!!bool" || got.Value != "false" {
		t.Fatalf("required = %#v, want false", got)
	}
	schema := mappingValue(param, "schema")
	if got := mappingValue(schema, "type"); got == nil || got.Value != "string" {
		t.Fatalf("parameter schema.type = %#v, want string", got)
	}
	if got := mappingValue(schema, "example"); got == nil || got.Value != currentPublicAPIVersion {
		t.Fatalf("parameter schema.example = %#v, want %q", got, currentPublicAPIVersion)
	}

	responses := mappingValue(op, "responses")
	deprecationHeader := mappingValue(mappingValue(mappingValue(responses, "200"), "headers"), apiDeprecationHeader)
	deprecationSchema := mappingValue(deprecationHeader, "schema")
	if got := mappingValue(deprecationSchema, "example"); got != nil {
		t.Fatalf("Deprecation example should be removed, got %#v", got)
	}

	errorResponse := mappingValue(mappingValue(mappingValue(root, "components"), "schemas"), "ErrorResponse")
	if got := mappingValue(errorResponse, "type"); got == nil || got.Value != "object" {
		t.Fatalf("ErrorResponse.type = %#v, want object", got)
	}
}

func TestPatchOpenAPI_StripsExternalURLs(t *testing.T) {
	t.Parallel()

	urlScheme := "https:" + "//"
	in := []byte(strings.NewReplacer(
		"{GNOSTIC_URL}", urlScheme+"github.com/google/gnostic/tree/master/cmd/protoc-gen-openapi",
		"{GRPC_URL}", urlScheme+"github.com/grpc",
		"{API_GUIDE_URL}", urlScheme+"cloud.google.com/apis/design/errors",
	).Replace(`# Generated with protoc-gen-openapi
# {GNOSTIC_URL}

openapi: 3.0.3
paths:
  /v1/test:
    get:
      responses:
        default:
          description: Default error response
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Status'
components:
  schemas:
    Status:
      type: object
      description: 'The Status type defines a logical error model that is suitable for different programming environments, including REST APIs and RPC APIs. It is used by gRPC. Each Status message contains three pieces of data: error code, error message, and error details. You can find out more about this error model and how to work with it in the API Design Guide.'
`))

	out, err := PatchOpenAPI(in)
	if err != nil {
		t.Fatalf("PatchOpenAPI: %v", err)
	}

	if strings.Contains(string(out), urlScheme) {
		t.Fatalf("PatchOpenAPI output still contains https URL:\n%s", out)
	}
	if strings.Contains(string(out), "http:"+"/"+"/") {
		t.Fatalf("PatchOpenAPI output still contains http URL:\n%s", out)
	}
}

func TestPatchOpenAPI_RejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	if _, err := PatchOpenAPI([]byte(":\n")); err == nil {
		t.Fatal("PatchOpenAPI accepted invalid YAML")
	}

	if _, err := documentMapping(ptrext.Of(yaml.Node{Kind: yaml.SequenceNode})); err == nil {
		t.Fatal("documentMapping accepted non-document node")
	}

	if _, err := documentMapping(ptrext.Of(yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{ptrext.Of(yaml.Node{Kind: yaml.SequenceNode})},
	})); err == nil {
		t.Fatal("documentMapping accepted non-mapping root")
	}
}

func TestPatchOpenAPI_SkipsInboundVersionHeadersAndMalformedOperations(t *testing.T) {
	t.Parallel()

	in := []byte(`openapi: 3.0.3
paths:
  /v1/inbound/slack:
    post:
      responses:
        "400":
          description: bad request
  /v1/no-responses:
    get:
      operationId: NoResponses
  /v1/non-operation:
    parameters:
      - name: ignored
components:
  schemas: {}
`)

	out, err := PatchOpenAPI(in)
	if err != nil {
		t.Fatalf("PatchOpenAPI: %v", err)
	}

	root := parseYAMLDoc(t, out)
	paths := mappingValue(root, "paths")
	inboundOp := mappingValue(mappingValue(paths, "/v1/inbound/slack"), "post")
	if got := mappingValue(inboundOp, "parameters"); got != nil {
		t.Fatalf("inbound operation should not receive API version parameters: %#v", got)
	}
	responses := mappingValue(inboundOp, "responses")
	assertSchemaRef(t, mappingValue(responses, "400"), "#/components/schemas/ErrorResponse")

	nonOperation := mappingValue(mappingValue(paths, "/v1/non-operation"), "parameters")
	if nonOperation == nil || nonOperation.Kind != yaml.SequenceNode {
		t.Fatalf("non-operation path item was unexpectedly rewritten: %#v", nonOperation)
	}
}

func TestOpenAPINodeHelpersRepairZeroKindNodes(t *testing.T) {
	t.Parallel()

	parent := mappingNode()
	appendMappingPair(parent, "existingMap", ptrext.Of(yaml.Node{}))
	appendMappingPair(parent, "existingSeq", ptrext.Of(yaml.Node{}))

	gotMap := ensureMapping(parent, "existingMap")
	if gotMap.Kind != yaml.MappingNode || gotMap.Tag != "!!map" {
		t.Fatalf("ensureMapping did not repair zero-kind node: %#v", gotMap)
	}

	gotSeq := ensureSequence(parent, "existingSeq")
	if gotSeq.Kind != yaml.SequenceNode || gotSeq.Tag != "!!seq" {
		t.Fatalf("ensureSequence did not repair zero-kind node: %#v", gotSeq)
	}

	setBool(parent, "flag", true)
	if got := mappingValue(parent, "flag"); got == nil || got.Value != "true" {
		t.Fatalf("setBool true = %#v, want true", got)
	}
	setBool(parent, "flag", false)
	if got := mappingValue(parent, "flag"); got == nil || got.Value != "false" {
		t.Fatalf("setBool false = %#v, want false", got)
	}

	deleteMappingKey(parent, "missing")
	deleteMappingKey(ptrext.Of(yaml.Node{Kind: yaml.SequenceNode}), "missing")

	if findHeaderParameter(nil, apiVersionHeaderName) != nil {
		t.Fatal("findHeaderParameter returned a parameter for nil input")
	}
	if findHeaderParameter(ptrext.Of(yaml.Node{Kind: yaml.MappingNode}), apiVersionHeaderName) != nil {
		t.Fatal("findHeaderParameter returned a parameter for non-sequence input")
	}
	if !isHTTPOperation("GET") {
		t.Fatal("isHTTPOperation should accept uppercase methods")
	}
	if isHTTPOperation("connect") {
		t.Fatal("isHTTPOperation should reject unsupported methods")
	}
}

func parseYAMLDoc(t *testing.T, raw []byte) *yaml.Node {
	t.Helper()

	doc := ptrext.Of(yaml.Node{})
	if err := yaml.Unmarshal(raw, doc); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	root, err := documentMapping(doc)
	if err != nil {
		t.Fatalf("documentMapping: %v", err)
	}
	return root
}

func assertSchemaRef(t *testing.T, response *yaml.Node, want string) {
	t.Helper()

	content := mappingValue(response, "content")
	if content == nil {
		t.Fatalf("response missing content: %#v", response)
	}
	appJSON := mappingValue(content, "application/json")
	if appJSON == nil {
		t.Fatalf("response missing application/json: %#v", response)
	}
	schema := mappingValue(appJSON, "schema")
	if schema == nil {
		t.Fatalf("response missing schema: %#v", response)
	}
	ref := mappingValue(schema, "$ref")
	if ref == nil {
		t.Fatalf("response missing $ref: %#v", response)
	}
	if ref.Value != want {
		t.Fatalf("$ref = %q, want %q", ref.Value, want)
	}
}

func assertHeaderParameter(t *testing.T, op *yaml.Node, want string) {
	t.Helper()

	parameters := mappingValue(op, "parameters")
	if parameters == nil || parameters.Kind != yaml.SequenceNode {
		t.Fatalf("operation missing parameters: %#v", op)
	}
	for _, item := range parameters.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		name := mappingValue(item, "name")
		in := mappingValue(item, "in")
		if name != nil && in != nil && name.Value == want && in.Value == "header" {
			return
		}
	}
	t.Fatalf("header parameter %q missing", want)
}

func assertResponseHeader(t *testing.T, response *yaml.Node, want string) {
	t.Helper()

	headers := mappingValue(response, "headers")
	if headers == nil {
		t.Fatalf("response missing headers: %#v", response)
	}
	header := mappingValue(headers, want)
	if header == nil {
		t.Fatalf("response missing header %q", want)
	}
	schema := mappingValue(header, "schema")
	if schema == nil {
		t.Fatalf("response header %q missing schema", want)
	}
	typ := mappingValue(schema, "type")
	if typ == nil || typ.Value != "string" {
		t.Fatalf("response header %q schema.type = %#v, want string", want, typ)
	}
}

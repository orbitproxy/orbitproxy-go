package toolschema

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

func TestSanitizeOmitsEmptyProperties(t *testing.T) {
	in := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[
		{"name":"browser_close","inputSchema":{"type":"object","properties":{}}},
		{"name":"browser_navigate","inputSchema":{"type":"object","properties":{"url":{"type":"string"}}}},
		{"name":"browser_null","inputSchema":{"type":"object","properties":null}}
	]}}`)
	out, changed := SanitizeToolsListJSON(in)
	if !changed {
		t.Fatal("expected sanitize change")
	}
	if bytes.Contains(out, []byte(`"properties":null`)) || bytes.Contains(out, []byte(`"properties":{}`)) {
		t.Fatalf("empty/null properties must be omitted: %s", out)
	}
	if !bytes.Contains(out, []byte(`"url":{"type":"string"}`)) {
		t.Fatalf("non-empty properties must remain: %s", out)
	}
}

func TestSanitizeNestedNullSchemasFromShell(t *testing.T) {
	in := []byte(`{"jsonrpc":"2.0","id":2,"result":{"tools":[
		{"name":"edit_block","inputSchema":{"type":"object","properties":{"file_path":{"type":"string"},"content":null,"options":{"type":"object","additionalProperties":null}}}},
		{"name":"read_file","inputSchema":{"type":"object","properties":{"path":{"type":"string"},"options":{"type":"object","additionalProperties":null}}}},
		{"name":"write_pdf","inputSchema":{"type":"object","properties":{"options":{"type":"object","properties":null},"content":{"anyOf":[{"type":"string"},{"type":"array","items":{"anyOf":[{"type":"object","properties":{"pdfOptions":{"type":"object","properties":null}}}]}}]}}}},
		{"name":"keep_legal","inputSchema":{"type":"object","properties":{"url":{"type":"string"},"label":{"type":"string","default":null}},"additionalProperties":false}}
	]}}`)
	out, changed := SanitizeToolsListJSON(in)
	if !changed {
		t.Fatal("expected sanitize change")
	}
	if bytes.Contains(out, []byte(`"content":null`)) || bytes.Contains(out, []byte(`"additionalProperties":null`)) {
		t.Fatalf("null schema nodes must be rewritten: %s", out)
	}
	if bytes.Contains(out, []byte(`"content":{}`)) {
		t.Fatalf("any schema must not be empty object: %s", out)
	}

	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	tools, _ := nested(root, "result", "tools").([]any)
	if len(tools) != 4 {
		t.Fatalf("tools = %d", len(tools))
	}

	editBlock := toolByName(t, tools, "edit_block")
	content, _ := nested(editBlock, "inputSchema", "properties", "content").(map[string]any)
	if !isJSONSchemaAny(content) {
		t.Fatalf("content must become non-empty any schema, got %#v", content)
	}
	if additional := nested(editBlock, "inputSchema", "properties", "options", "additionalProperties"); additional != true {
		t.Fatalf("edit_block additionalProperties must become true, got %#v", additional)
	}

	readFile := toolByName(t, tools, "read_file")
	if additional := nested(readFile, "inputSchema", "properties", "options", "additionalProperties"); additional != true {
		t.Fatalf("read_file additionalProperties must become true, got %#v", additional)
	}

	writePDF := toolByName(t, tools, "write_pdf")
	options, _ := nested(writePDF, "inputSchema", "properties", "options").(map[string]any)
	if _, hasProps := options["properties"]; hasProps {
		t.Fatalf("write_pdf options.properties:null must be omitted: %#v", options)
	}

	keep := toolByName(t, tools, "keep_legal")
	if additional := nested(keep, "inputSchema", "additionalProperties"); additional != false {
		t.Fatalf("legal additionalProperties:false must remain, got %#v", additional)
	}
	label, _ := nested(keep, "inputSchema", "properties", "label").(map[string]any)
	if _, exists := label["default"]; !exists {
		t.Fatal("default key must remain")
	}
	if label["default"] != nil {
		t.Fatalf("default:null must remain, got %#v", label["default"])
	}
}

func TestSanitizeAfterMessageMarshal(t *testing.T) {
	type message struct {
		JSONRPC json.RawMessage `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
	}
	in := message{
		JSONRPC: json.RawMessage(`"2.0"`),
		ID:      json.RawMessage(`2`),
		Result:  json.RawMessage(`{"tools":[{"name":"edit_block","inputSchema":{"type":"object","properties":{"file_path":{"type":"string"},"content":null,"options":{"additionalProperties":null}}}}]}`),
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, changed := SanitizeToolsListJSON(body)
	if !changed {
		t.Fatalf("bridge-shaped envelope must sanitize, body=%s", body)
	}
	if bytes.Contains(out, []byte(`"content":null`)) || bytes.Contains(out, []byte(`"additionalProperties":null`)) {
		t.Fatalf("null schema nodes remain: %s", out)
	}
}

func TestSanitizePublicToolsListExactBytes(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "public_tools_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 76029 {
		t.Fatalf("fixture size = %d, want the captured public body 76029", len(in))
	}

	before := decodeToolsList(t, in)
	if len(before) != 46 {
		t.Fatalf("fixture tools = %d, want 46", len(before))
	}
	beforeIllegal := illegalSchemaNulls(before)
	if len(beforeIllegal) == 0 {
		t.Fatal("fixture must still contain the captured illegal null schema nodes")
	}
	wantBefore := []string{
		"orbitproxy-test-shell__edit_block inputSchema/properties/content",
		"orbitproxy-test-shell__edit_block inputSchema/properties/options/additionalProperties",
		"orbitproxy-test-shell__read_file inputSchema/properties/options/additionalProperties",
	}
	sort.Strings(wantBefore)
	if got := joinPaths(beforeIllegal); got != joinPaths(wantBefore) {
		t.Fatalf("fixture illegal nulls changed\ngot:\n%s\nwant:\n%s", got, joinPaths(wantBefore))
	}
	if countDefaultNulls(before) == 0 {
		t.Fatal("fixture must keep docker default:null so we can prove they stay")
	}

	out, changed := SanitizeToolsListJSON(in)
	if !changed {
		t.Fatal("captured public tools/list must be rewritten")
	}

	after := decodeToolsList(t, out)
	if len(after) != 46 {
		t.Fatalf("tools after sanitize = %d, want 46", len(after))
	}
	if got, want := toolNames(after), toolNames(before); !stringSlicesEqual(got, want) {
		t.Fatalf("tool names must stay in order\ngot %v\nwant %v", got, want)
	}

	afterIllegal := illegalSchemaNulls(after)
	if len(afterIllegal) != 0 {
		t.Fatalf("illegal schema nulls remain:\n%s", joinPaths(afterIllegal))
	}
	if countDefaultNulls(after) != countDefaultNulls(before) {
		t.Fatalf("default:null count changed: before %d after %d", countDefaultNulls(before), countDefaultNulls(after))
	}

	editBlock := toolByName(t, toolsAsAny(after), "orbitproxy-test-shell__edit_block")
	content, _ := nested(editBlock, "inputSchema", "properties", "content").(map[string]any)
	if !isJSONSchemaAny(content) {
		t.Fatalf("edit_block.content must become non-empty any schema, got %#v", content)
	}
	if additional := nested(editBlock, "inputSchema", "properties", "options", "additionalProperties"); additional != true {
		t.Fatalf("edit_block options.additionalProperties must become true, got %#v", additional)
	}
	readFile := toolByName(t, toolsAsAny(after), "orbitproxy-test-shell__read_file")
	if additional := nested(readFile, "inputSchema", "properties", "options", "additionalProperties"); additional != true {
		t.Fatalf("read_file options.additionalProperties must become true, got %#v", additional)
	}
}

func TestSanitizeEdgeShellToolsListExactBytes(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "edge_shell_tools_list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 61089 {
		t.Fatalf("fixture size = %d, want the captured Edge body 61089", len(in))
	}

	before := decodeToolsList(t, in)
	if len(before) != 26 {
		t.Fatalf("fixture tools = %d, want 26", len(before))
	}
	beforeEmpty := emptySchemaObjects(before)
	wantBefore := []string{
		"edit_block inputSchema/properties/content",
		"edit_block inputSchema/properties/options/additionalProperties",
		"read_file inputSchema/properties/options/additionalProperties",
	}
	sort.Strings(wantBefore)
	if got := joinPaths(beforeEmpty); got != joinPaths(wantBefore) {
		t.Fatalf("fixture empty schema objects changed\ngot:\n%s\nwant:\n%s", got, joinPaths(wantBefore))
	}
	if len(illegalSchemaNulls(before)) != 0 {
		t.Fatal("Edge fixture must have no illegal nulls before aggregation")
	}

	out, changed := SanitizeToolsListJSON(in)
	if !changed {
		t.Fatal("captured Edge tools/list must be rewritten")
	}

	after := decodeToolsList(t, out)
	if len(after) != 26 {
		t.Fatalf("tools after sanitize = %d, want 26", len(after))
	}
	if got, want := toolNames(after), toolNames(before); !stringSlicesEqual(got, want) {
		t.Fatalf("tool names must stay in order\ngot %v\nwant %v", got, want)
	}
	if remain := emptySchemaObjects(after); len(remain) != 0 {
		t.Fatalf("empty schema objects remain:\n%s", joinPaths(remain))
	}
	if remain := illegalSchemaNulls(after); len(remain) != 0 {
		t.Fatalf("illegal schema nulls remain:\n%s", joinPaths(remain))
	}

	editBlock := toolByName(t, toolsAsAny(after), "edit_block")
	content, _ := nested(editBlock, "inputSchema", "properties", "content").(map[string]any)
	if !isJSONSchemaAny(content) {
		t.Fatalf("edit_block.content must become non-empty any schema, got %#v", content)
	}
	if additional := nested(editBlock, "inputSchema", "properties", "options", "additionalProperties"); additional != true {
		t.Fatalf("edit_block options.additionalProperties must become true, got %#v", additional)
	}
	readFile := toolByName(t, toolsAsAny(after), "read_file")
	if additional := nested(readFile, "inputSchema", "properties", "options", "additionalProperties"); additional != true {
		t.Fatalf("read_file options.additionalProperties must become true, got %#v", additional)
	}
}

func TestSanitizeEmptyObjectPropertySameAsNull(t *testing.T) {
	in, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"result": map[string]any{
			"tools": []any{
				map[string]any{
					"name": "edit_block",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content": map[string]any{},
							"options": map[string]any{
								"type":                 "object",
								"additionalProperties": map[string]any{},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, changed := SanitizeToolsListJSON(in)
	if !changed {
		t.Fatal("empty schema objects must be rewritten")
	}
	if bytes.Contains(out, []byte(`"content":{}`)) || bytes.Contains(out, []byte(`"additionalProperties":{}`)) {
		t.Fatalf("empty schema objects remain: %s", out)
	}
	after := decodeToolsList(t, out)
	editBlock := toolByName(t, toolsAsAny(after), "edit_block")
	content, _ := nested(editBlock, "inputSchema", "properties", "content").(map[string]any)
	if !isJSONSchemaAny(content) {
		t.Fatalf("content {} must become non-empty any schema, got %#v", content)
	}
	if additional := nested(editBlock, "inputSchema", "properties", "options", "additionalProperties"); additional != true {
		t.Fatalf("additionalProperties {} must become true, got %#v", additional)
	}
}

func TestSanitizeLegalSchemaUnchanged(t *testing.T) {
	in := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"query","inputSchema":{"type":"object","properties":{"sql":{"type":"string"}},"required":["sql"],"additionalProperties":false}}]}}`)
	out, changed := SanitizeToolsListJSON(in)
	if changed {
		t.Fatalf("legal schema must stay unchanged: %s", out)
	}
}

func decodeToolsList(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatal(err)
	}
	result, _ := root["result"].(map[string]any)
	raw, _ := result["tools"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		tool, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("tool is %T", item)
		}
		out = append(out, tool)
	}
	return out
}

func toolsAsAny(tools []map[string]any) []any {
	out := make([]any, len(tools))
	for i := range tools {
		out[i] = tools[i]
	}
	return out
}

func toolNames(tools []map[string]any) []string {
	out := make([]string, len(tools))
	for i, tool := range tools {
		name, _ := tool["name"].(string)
		out[i] = name
	}
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinPaths(paths []string) string {
	var buf bytes.Buffer
	for i, path := range paths {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(path)
	}
	return buf.String()
}

func countDefaultNulls(tools []map[string]any) int {
	count := 0
	for _, tool := range tools {
		walkNulls(tool["inputSchema"], "inputSchema", func(path string, key string) {
			if key == "default" {
				count++
			}
		})
	}
	return count
}

func emptySchemaObjects(tools []map[string]any) []string {
	var out []string
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		walkEmptyObjects(tool["inputSchema"], "inputSchema", func(path string) {
			out = append(out, name+" "+path)
		})
	}
	sort.Strings(out)
	return out
}

func walkEmptyObjects(value any, path string, visit func(path string)) {
	m, ok := value.(map[string]any)
	if !ok {
		if arr, isArr := value.([]any); isArr {
			for i, child := range arr {
				walkEmptyObjects(child, path+"/"+itoa(i), visit)
			}
		}
		return
	}
	if len(m) == 0 {
		visit(path)
		return
	}
	for key, child := range m {
		walkEmptyObjects(child, path+"/"+key, visit)
	}
}

func illegalSchemaNulls(tools []map[string]any) []string {
	var out []string
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		walkNulls(tool["inputSchema"], "inputSchema", func(path string, key string) {
			if key == "default" {
				return
			}
			out = append(out, name+" "+path)
		})
	}
	sort.Strings(out)
	return out
}

func walkNulls(value any, path string, visit func(path string, key string)) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := path + "/" + key
			if child == nil {
				visit(childPath, key)
				continue
			}
			walkNulls(child, childPath, visit)
		}
	case []any:
		for i, child := range typed {
			walkNulls(child, path+"/"+itoa(i), visit)
		}
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func isJSONSchemaAny(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	types, ok := schema["type"].([]any)
	if !ok || len(types) != 6 {
		return false
	}
	return true
}

func toolByName(t *testing.T, tools []any, name string) map[string]any {
	t.Helper()
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		if tool["name"] == name {
			return tool
		}
	}
	t.Fatalf("missing tool %s", name)
	return nil
}

func nested(root any, keys ...string) any {
	cur := root
	for _, key := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[key]
	}
	return cur
}

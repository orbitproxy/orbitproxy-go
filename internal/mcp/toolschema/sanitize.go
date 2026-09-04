// Package toolschema 清洗 MCP tools/list 里的非法 JSON Schema 节点。
//
// 挂在 Machine SDK 写出 tools/list 的路径上（stdio 桥、discover），不进 Edge。
// 空 object 在聚合后会变成 null，因此「any」不能用 {}。
package toolschema

import "encoding/json"

// jsonSchemaAny 是带字段的 JSON Schema any，对应 z.any()。
// 不用 {}：聚合后会被编成 null，Cursor 会整表拒收 tools。
func jsonSchemaAny() map[string]any {
	return map[string]any{
		"type": []any{"object", "string", "number", "boolean", "array", "null"},
	}
}

// isEmptyObject 是会变成 null 的空 schema。和 JSON null 同等处理。
func isEmptyObject(value any) bool {
	m, ok := value.(map[string]any)
	return ok && len(m) == 0
}

// SanitizeToolsListJSON 清洗 JSON-RPC tools/list 的 result.tools[].inputSchema。
// 不是该形状则原样返回。
func SanitizeToolsListJSON(body []byte) ([]byte, bool) {
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return body, false
	}
	resultRaw, ok := root["result"]
	if !ok {
		return body, false
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(resultRaw, &result) != nil {
		return body, false
	}
	toolsRaw, ok := result["tools"]
	if !ok {
		return body, false
	}
	var tools []map[string]any
	if json.Unmarshal(toolsRaw, &tools) != nil {
		return body, false
	}
	changed := false
	for i := range tools {
		if SanitizeToolInputSchema(tools[i]) {
			changed = true
		}
	}
	if !changed {
		return body, false
	}
	toolsFixed, err := json.Marshal(tools)
	if err != nil {
		return body, false
	}
	result["tools"] = toolsFixed
	resultFixed, err := json.Marshal(result)
	if err != nil {
		return body, false
	}
	root["result"] = resultFixed
	out, err := json.Marshal(root)
	if err != nil {
		return body, false
	}
	return out, true
}

// SanitizeToolInputSchema 清洗单个 tool 的 inputSchema。
// 按 schema 位置统一替换 null 和 {}，不认具体 property 名。
// {} 会变成 null；default:null 不是 schema 节点，不动。
func SanitizeToolInputSchema(tool map[string]any) bool {
	raw, exists := tool["inputSchema"]
	if !exists {
		return false
	}
	if raw == nil || isEmptyObject(raw) {
		tool["inputSchema"] = map[string]any{"type": "object"}
		return true
	}
	schema, ok := raw.(map[string]any)
	if !ok || schema == nil {
		return false
	}
	return sanitizeJSONSchema(schema)
}

func sanitizeJSONSchema(schema map[string]any) bool {
	changed := sanitizeProperties(schema)
	if sanitizeAdditionalProperties(schema) {
		changed = true
	}
	if sanitizeSchemaKeyword(schema, "items") {
		changed = true
	}
	if sanitizeSchemaKeyword(schema, "not") {
		changed = true
	}
	if sanitizeSchemaSliceKeyword(schema, "anyOf") {
		changed = true
	}
	if sanitizeSchemaSliceKeyword(schema, "oneOf") {
		changed = true
	}
	if sanitizeSchemaSliceKeyword(schema, "allOf") {
		changed = true
	}
	if sanitizeSchemaSliceKeyword(schema, "prefixItems") {
		changed = true
	}
	if sanitizeSchemaMapKeyword(schema, "$defs") {
		changed = true
	}
	if sanitizeSchemaMapKeyword(schema, "definitions") {
		changed = true
	}
	if sanitizeSchemaMapKeyword(schema, "patternProperties") {
		changed = true
	}
	return changed
}

func sanitizeProperties(schema map[string]any) bool {
	props, exists := schema["properties"]
	if !exists {
		return false
	}
	if props == nil {
		delete(schema, "properties")
		return true
	}
	m, ok := props.(map[string]any)
	if !ok {
		return false
	}
	if len(m) == 0 {
		delete(schema, "properties")
		return true
	}
	changed := false
	for key, value := range m {
		if value == nil || isEmptyObject(value) {
			m[key] = jsonSchemaAny()
			changed = true
			continue
		}
		child, isObject := value.(map[string]any)
		if !isObject {
			continue
		}
		if sanitizeJSONSchema(child) {
			changed = true
		}
	}
	return changed
}

func sanitizeAdditionalProperties(schema map[string]any) bool {
	value, exists := schema["additionalProperties"]
	if !exists {
		return false
	}
	if value == nil || isEmptyObject(value) {
		schema["additionalProperties"] = true
		return true
	}
	child, ok := value.(map[string]any)
	if !ok {
		return false
	}
	return sanitizeJSONSchema(child)
}

func sanitizeSchemaKeyword(schema map[string]any, key string) bool {
	value, exists := schema[key]
	if !exists {
		return false
	}
	if value == nil || isEmptyObject(value) {
		schema[key] = jsonSchemaAny()
		return true
	}
	if child, ok := value.(map[string]any); ok {
		return sanitizeJSONSchema(child)
	}
	if arr, ok := value.([]any); ok {
		return sanitizeSchemaSlice(arr)
	}
	return false
}

func sanitizeSchemaSliceKeyword(schema map[string]any, key string) bool {
	value, exists := schema[key]
	if !exists {
		return false
	}
	arr, ok := value.([]any)
	if !ok {
		return false
	}
	return sanitizeSchemaSlice(arr)
}

func sanitizeSchemaSlice(arr []any) bool {
	changed := false
	for i, value := range arr {
		if value == nil || isEmptyObject(value) {
			arr[i] = jsonSchemaAny()
			changed = true
			continue
		}
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if sanitizeJSONSchema(child) {
			changed = true
		}
	}
	return changed
}

func sanitizeSchemaMapKeyword(schema map[string]any, key string) bool {
	value, exists := schema[key]
	if !exists {
		return false
	}
	m, ok := value.(map[string]any)
	if !ok {
		return false
	}
	changed := false
	for childKey, childValue := range m {
		if childValue == nil || isEmptyObject(childValue) {
			m[childKey] = jsonSchemaAny()
			changed = true
			continue
		}
		child, isObject := childValue.(map[string]any)
		if !isObject {
			continue
		}
		if sanitizeJSONSchema(child) {
			changed = true
		}
	}
	return changed
}

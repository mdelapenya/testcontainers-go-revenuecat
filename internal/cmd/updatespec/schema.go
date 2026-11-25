package main

import "strings"

func asObject(value any) object {
	obj, _ := value.(map[string]any)
	return obj
}

func dereference(root, node object) object {
	ref, ok := node["$ref"].(string)
	if !ok {
		return node
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	for _, token := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		root = asObject(root[token])
	}
	return root
}

func example(schema object) (any, bool) {
	for _, key := range []string{"const", "example", "default"} {
		if value, ok := schema[key]; ok {
			return value, true
		}
	}
	for _, key := range []string{"examples", "enum"} {
		if values, ok := schema[key].([]any); ok && len(values) > 0 {
			return values[0], true
		}
	}
	return nil, false
}

func singlePage(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(object, len(value))
		for name, field := range value {
			if name != "next_page" || value["object"] != "list" {
				result[name] = singlePage(field)
			}
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = singlePage(item)
		}
		return result
	default:
		return value
	}
}

// sample generates a small representative value for the JSON Schema shapes
// used in the RevenueCat documentation.
func sample(root, schema object, depth int) any {
	if schema == nil || depth >= 20 {
		return nil
	}
	if _, ok := schema["$ref"]; ok {
		return sample(root, dereference(root, schema), depth+1)
	}
	if value, ok := example(schema); ok {
		return value
	}

	typeName, _ := schema["type"].(string)
	if types, ok := schema["type"].([]any); ok {
		for _, candidate := range types {
			if candidate != "null" {
				typeName, _ = candidate.(string)
				break
			}
		}
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		variants, _ := schema[key].([]any)
		if len(variants) != 0 {
			return sample(root, asObject(variants[0]), depth+1)
		}
	}

	switch typeName {
	case "", "object":
		result := object{}
		parts, _ := schema["allOf"].([]any)
		for _, part := range parts {
			for name, value := range asObject(sample(root, asObject(part), depth+1)) {
				result[name] = value
			}
		}
		properties := asObject(schema["properties"])
		required, _ := schema["required"].([]any)
		for _, key := range required {
			name, _ := key.(string)
			if property, ok := properties[name]; ok {
				result[name] = sample(root, asObject(property), depth+1)
			}
		}
		return result
	case "array":
		if value := sample(root, asObject(schema["items"]), depth+1); value != nil {
			return []any{value}
		}
		return []any{}
	case "string":
		switch schema["format"] {
		case "date-time":
			return "2025-01-01T00:00:00Z"
		case "uri", "url":
			return "https://example.invalid/"
		case "uuid":
			return "123e4567-e89b-12d3-a456-426614174000"
		}
		return "example"
	case "integer", "number":
		if minimum, ok := schema["minimum"]; ok {
			return minimum
		}
		return 1
	case "boolean":
		return false
	default:
		return nil
	}
}

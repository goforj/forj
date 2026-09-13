// Package stacks manages reusable resource configurations without moving application data.
package stacks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/goforj/goforj/internal/envfile"
	"github.com/joho/godotenv"
)

// document retains original assignment spans so unrelated owner text survives activation.
type document struct {
	raw    []byte
	values map[string]string
	chunks []chunk
}

// chunk includes an entire multiline assignment and its original line endings.
type chunk struct{ key, text string }

// parseDocument delegates dotenv semantics while retaining complete assignment boundaries.
func parseDocument(raw []byte) (document, error) {
	d := document{raw: raw}
	if err := envfile.ValidatePortableDocument(raw); err != nil {
		return d, err
	}
	values, err := godotenv.Unmarshal(string(raw))
	if err != nil {
		return d, err
	}
	d.values = values
	lines := strings.SplitAfter(string(raw), "\n")
	for i := 0; i < len(lines); i++ {
		text := lines[i]
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			d.chunks = append(d.chunks, chunk{text: text})
			continue
		}
		key, ok := envfile.ScanKey(text)
		if !ok {
			return d, fmt.Errorf("unsupported dotenv assignment")
		}
		for {
			_, err := godotenv.Unmarshal(text)
			if err == nil {
				break
			}
			i++
			if i >= len(lines) {
				return d, fmt.Errorf("invalid dotenv assignment for %s", key)
			}
			text += lines[i]
		}
		d.chunks = append(d.chunks, chunk{key: key, text: text})
	}
	return d, nil
}

// replace updates only managed assignments, including removal of keys absent from the target.
func (d document) replace(managed func(string) bool, values map[string]string) []byte {
	var out strings.Builder
	ending := "\n"
	if strings.Contains(string(d.raw), "\r\n") {
		ending = "\r\n"
	}
	remaining := clone(values)
	for _, part := range d.chunks {
		if part.key == "" || !managed(part.key) {
			out.WriteString(part.text)
			continue
		}
		if value, ok := remaining[part.key]; ok {
			out.WriteString(part.key + "=" + envfile.EncodeValue(value) + ending)
			delete(remaining, part.key)
		}
	}
	if len(remaining) > 0 && out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
		out.WriteString(ending)
	}
	for _, key := range keys(remaining) {
		out.WriteString(key + "=" + envfile.EncodeValue(remaining[key]) + ending)
	}
	return []byte(out.String())
}

// encodeDocument produces deterministic dotenv files with literal, round-trippable values.
func encodeDocument(values map[string]string) []byte {
	var out strings.Builder
	for _, key := range keys(values) {
		out.WriteString(key + "=" + envfile.EncodeValue(values[key]) + "\n")
	}
	return []byte(out.String())
}

// keys makes file output and previews deterministic.
func keys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

// clone separates editable configurations from recovery snapshots.
func clone(values map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range values {
		result[key] = value
	}
	return result
}

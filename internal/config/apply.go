// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// ApplyPatch is an additive edit to a Veduta configuration. Maps are used at this boundary so
// importers can write only public YAML fields without depending on config's decoded union types.
type ApplyPatch struct {
	Connections  map[string]map[string]any
	Integrations []map[string]any
	Sections     []map[string]any
}

// Apply edits a config's YAML node tree, preserving comments and scalar/collection styles, then
// validates the complete primary+conf.d result before atomically replacing the primary file.
func Apply(path string, patch ApplyPatch) error {
	body, err := os.ReadFile(path) // #nosec G304 -- operator-supplied config destination.
	if err != nil {
		return err
	}
	var document yaml.Node
	if err = yaml.Unmarshal(body, &document); err != nil {
		return fmt.Errorf("parse config before apply: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return errors.New("config apply: top level must be a mapping")
	}
	root := document.Content[0]
	if err = appendConnections(root, patch.Connections); err != nil {
		return err
	}
	if err = appendSequenceValues(root, "integrations", patch.Integrations); err != nil {
		return err
	}
	if err = mergeSections(root, patch.Sections); err != nil {
		return err
	}

	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err = encoder.Encode(&document); err != nil {
		return fmt.Errorf("encode applied config: %w", err)
	}
	if err = encoder.Close(); err != nil {
		return fmt.Errorf("close config encoder: %w", err)
	}
	return validateAndReplace(path, encoded.Bytes())
}

func appendConnections(root *yaml.Node, additions map[string]map[string]any) error {
	if len(additions) == 0 {
		return nil
	}
	target, err := mappingValue(root, "connections", yaml.MappingNode)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(additions))
	for id := range additions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		value := additions[id]
		if mapLookup(target, id) != nil {
			return fmt.Errorf("config apply: connection %q already exists", id)
		}
		target.Content = append(target.Content, scalarNode(id), encodedNode(value))
	}
	return nil
}

func appendSequenceValues(root *yaml.Node, key string, additions []map[string]any) error {
	if len(additions) == 0 {
		return nil
	}
	target, err := mappingValue(root, key, yaml.SequenceNode)
	if err != nil {
		return err
	}
	for _, value := range additions {
		target.Content = append(target.Content, encodedNode(value))
	}
	return nil
}

func mergeSections(root *yaml.Node, additions []map[string]any) error {
	if len(additions) == 0 {
		return nil
	}
	target, err := mappingValue(root, "sections", yaml.SequenceNode)
	if err != nil {
		return err
	}
	for _, value := range additions {
		title, _ := value["title"].(string)
		var existing *yaml.Node
		for _, section := range target.Content {
			if scalarValue(mapLookup(section, "title")) == title {
				existing = section
				break
			}
		}
		if existing == nil {
			target.Content = append(target.Content, encodedNode(value))
			continue
		}
		cards, cardErr := mappingValue(existing, "cards", yaml.SequenceNode)
		if cardErr != nil {
			return cardErr
		}
		for _, card := range anySlice(value["cards"]) {
			cards.Content = append(cards.Content, encodedNode(card))
		}
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string, kind yaml.Kind) (*yaml.Node, error) {
	if mapping.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config apply: %s parent is not a mapping", key)
	}
	if found := mapLookup(mapping, key); found != nil {
		if found.Kind != kind {
			return nil, fmt.Errorf("config apply: %s has the wrong YAML shape", key)
		}
		return found, nil
	}
	created := &yaml.Node{Kind: kind, Tag: map[yaml.Kind]string{yaml.MappingNode: "!!map", yaml.SequenceNode: "!!seq"}[kind]}
	mapping.Content = append(mapping.Content, scalarNode(key), created)
	return created, nil
}

func mapLookup(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func scalarValue(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	return node.Value
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func encodedNode(value any) *yaml.Node {
	node := &yaml.Node{}
	if err := node.Encode(value); err != nil {
		panic("config apply: value cannot fail YAML encoding: " + err.Error())
	}
	return node
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func validateAndReplace(path string, body []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".veduta-import-*.yaml")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err = temporary.Chmod(info.Mode().Perm()); err == nil {
		_, err = temporary.Write(body)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if _, diagnostics := LoadPath(temporaryPath); diagnostics.HasErrors() {
		return fmt.Errorf("config apply produced invalid configuration:\n%s", diagnostics.String())
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

// SPDX-License-Identifier: AGPL-3.0-or-later

package declarative

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func decodeJSON(raw []byte, b *budget, maxDepth int) (any, error) {
	b.mu.Lock()
	if len(raw) > b.maxBytes-b.bytes {
		b.mu.Unlock()
		return nil, fmt.Errorf("input byte budget exceeded")
	}
	b.bytes += len(raw)
	b.mu.Unlock()
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	depth, nodes := 0, 0
	for {
		tok, e := d.Token()
		if e != nil {
			break
		}
		nodes++
		switch x := tok.(type) {
		case json.Delim:
			if x == '{' || x == '[' {
				depth++
				if depth > maxDepth {
					return nil, fmt.Errorf("JSON depth exceeds %d", maxDepth)
				}
			} else {
				depth--
			}
		case string:
			if len(x) > 1<<20 {
				return nil, fmt.Errorf("JSON token exceeds 1 MiB")
			}
		}
		b.mu.Lock()
		if b.nodes+nodes > b.maxNodes {
			b.mu.Unlock()
			return nil, fmt.Errorf("JSON node budget exceeded")
		}
		b.mu.Unlock()
	}
	b.mu.Lock()
	b.nodes += nodes
	b.mu.Unlock()
	var out any
	d = json.NewDecoder(bytes.NewReader(raw))
	if e := d.Decode(&out); e != nil {
		return nil, e
	}
	return out, nil
}

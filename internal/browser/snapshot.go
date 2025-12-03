package browser

import (
	"fmt"
)

type ElementInfo struct {
	ID       string `json:"id"`
	Tag      string `json:"tag"`
	Role     string `json:"role"`
	Text     string `json:"text"`
	Selector string `json:"selector"`
}

type PageSnapshot struct {
	URL      string        `json:"url"`
	Title    string        `json:"title"`
	Elements []ElementInfo `json:"elements"`
}

func (m *Manager) Snapshot(limit int) (*PageSnapshot, error) {
	if m == nil || m.Page == nil {
		return nil, fmt.Errorf("page is not initialized")
	}

	// В playwright-go URL обычно не возвращает ошибку.
	url := m.Page.URL()

	title, err := m.Page.Title()
	if err != nil {
		return nil, fmt.Errorf("could not get page title: %w", err)
	}

	script := `(limit) => {
		const elements = [];
		const candidates = Array.from(
			document.querySelectorAll('a, button, input, [role="button"], [role="link"]')
		);

		for (const el of candidates) {
			if (elements.length >= limit) break;

			const rect = el.getBoundingClientRect();

			if (rect.width === 0 || rect.height === 0) continue;

			const text = (el.innerText || el.getAttribute('aria-label') || el.getAttribute('title') || '').trim();
			if (!text) continue;

			let selector = el.tagName.toLowerCase();
			if (el.id) {
				selector += '#' + el.id;
			} else if (el.className && typeof el.className === 'string') {
				const cls = el.className.split(/\s+/).filter(Boolean)[0];
				if (cls) {
					selector += '.' + cls;
				}
			}

			elements.push({
				tag: el.tagName.toLowerCase(),
				role: el.getAttribute('role') || '',
				text,
				selector,
			});
		}

		return elements;
	}`

	raw, err := m.Page.Evaluate(script, limit)
	if err != nil {
		return nil, fmt.Errorf("could not evaluate DOM extraction script: %w", err)
	}

	rawSlice, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected JS result type: %T", raw)
	}

	elements := make([]ElementInfo, 0, len(rawSlice))

	for i, item := range rawSlice {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		el := ElementInfo{
			ID:       fmt.Sprintf("el_%d", i),
			Tag:      strFromMap(obj, "tag"),
			Role:     strFromMap(obj, "role"),
			Text:     strFromMap(obj, "text"),
			Selector: strFromMap(obj, "selector"),
		}

		elements = append(elements, el)
	}

	return &PageSnapshot{
		URL:      url,
		Title:    title,
		Elements: elements,
	}, nil
}

func strFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

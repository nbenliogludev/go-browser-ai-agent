package browser

import (
	"fmt"
	"strings"
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

	url := m.Page.URL()

	title, err := m.Page.Title()
	if err != nil {
		return nil, fmt.Errorf("could not get page title: %w", err)
	}

	script := `(limit) => {
		const elements = [];
		const candidates = Array.from(
			document.querySelectorAll('a, button, input, textarea, [role="button"], [role="link"]')
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

// ExtractText — универсальный helper для чтения длинного текста по selector.
func (m *Manager) ExtractText(selector string, maxLen int) (string, error) {
	if m == nil || m.Page == nil {
		return "", fmt.Errorf("page is not initialized")
	}

	text, err := m.Page.TextContent(selector)
	if err != nil {
		return "", fmt.Errorf("could not get text content for selector %q: %w", selector, err)
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}

	if maxLen > 0 {
		runes := []rune(text)
		if len(runes) > maxLen {
			text = string(runes[:maxLen])
		}
	}

	return text, nil
}

func strFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (m *Manager) ReadContent(selector string, maxChars int) (string, error) {
	if m == nil || m.Page == nil {
		return "", fmt.Errorf("page is not initialized")
	}
	if selector == "" {
		return "", fmt.Errorf("empty selector for ReadContent")
	}
	if maxChars <= 0 {
		maxChars = 2000
	}

	script := `(selector, maxChars) => {
		const normalize = (txt) => (txt || "").replace(/\s+/g, " ").trim();

		let el = document.querySelector(selector);
		if (!el) return "";

		let bestText = normalize(el.innerText || el.textContent);

		// Если текста мало – пробуем подняться по DOM и взять более крупный блок.
		let parent = el.parentElement;
		while (parent && bestText.length < maxChars) {
			const parentText = normalize(parent.innerText || parent.textContent);
			// Берем более длинный и содержательный текст.
			if (parentText.length > bestText.length) {
				bestText = parentText;
			}
			parent = parent.parentElement;
		}

		if (bestText.length > maxChars) {
			return bestText.slice(0, maxChars);
		}
		return bestText;
	}`

	raw, err := m.Page.Evaluate(script, selector, maxChars)
	if err != nil {
		return "", fmt.Errorf("could not evaluate content extraction script: %w", err)
	}

	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("unexpected JS result type for content: %T", raw)
	}

	return text, nil
}

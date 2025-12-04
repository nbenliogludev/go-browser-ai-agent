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
			document.querySelectorAll(
				'a, button, input, textarea, [role="button"], [role="link"], [role="textbox"]'
			)
		);

		for (const el of candidates) {
			if (elements.length >= limit) break;

			const rect = el.getBoundingClientRect();
			// пропускаем невидимые элементы
			if (rect.width === 0 || rect.height === 0) continue;

			const tag = el.tagName.toLowerCase();

			// текстовые input'ы (куда можно вводить текст)
			let isTextInput = false;
			if (tag === 'input') {
				const type = (el.getAttribute('type') || 'text').toLowerCase();
				const allowed = ['text', 'search', 'email', 'password', 'url', 'number'];
				if (allowed.includes(type)) {
					isTextInput = true;
				}
			} else if (tag === 'textarea') {
				// textarea — всегда текстовый ввод
				isTextInput = true;
			}

			// учитываем текущее value для input/textarea,
			// чтобы LLM видел введённый текст
			const value = (tag === 'input' || tag === 'textarea') ? (el.value || '') : '';

			const rawText =
				value ||                           // сначала текущее значение поля
				el.innerText ||
				el.getAttribute('aria-label') ||
				el.getAttribute('title') ||
				el.getAttribute('value') ||
				'';

			const text = rawText.trim();

			// для кнопок/ссылок без текста — пропускаем,
			// а вот текстовые поля (input/textarea) считаем интересными даже без текста
			if (!text && !isTextInput && tag !== 'textarea') continue;

			// базовый селектор: tag + id/class
			let selector = tag;
			if (el.id) {
				selector += '#' + el.id;
			} else if (el.className && typeof el.className === 'string') {
				const cls = el.className.split(/\s+/).filter(Boolean)[0];
				if (cls) {
					selector += '.' + cls;
				}
			}

			// роль: либо из атрибута, либо textbox для текстовых input/textarea
			let role = el.getAttribute('role') || '';
			if (!role && isTextInput) {
				role = 'textbox';
			}

			elements.push({
				tag,
				role,
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

package browser

import (
	"fmt"
)

type PageSnapshot struct {
	URL   string
	Title string
	Tree  string
}

// Snapshot выполняет JS на странице, размечает элементы ID-шниками и возвращает дерево.
func (m *Manager) Snapshot() (*PageSnapshot, error) {
	if m == nil || m.Page == nil {
		return nil, fmt.Errorf("page is not initialized")
	}

	// ИСПРАВЛЕНИЕ:
	// В JS коде ниже мы заменили `${var}` на конкатенацию `+ var +`,
	// чтобы избежать конфликтов с Go raw strings.

	script := `() => {
		let idCounter = 1;
		const interactiveTags = new Set(['a', 'button', 'input', 'textarea', 'select', 'details', 'summary']);
		
		function isInteractive(el) {
			const tag = el.tagName.toLowerCase();
			const role = el.getAttribute('role');
			const tabIndex = el.getAttribute('tabindex');
			
			return interactiveTags.has(tag) || 
				   role === 'button' || 
				   role === 'link' || 
				   role === 'checkbox' ||
				   role === 'menuitem' ||
				   role === 'tab' ||
				   role === 'textbox' ||
				   (tabIndex !== null && tabIndex !== '-1') ||
				   el.onclick != null;
		}

		function isVisible(el) {
			if (!el.getBoundingClientRect) return false;
			const rect = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			return rect.width > 0 && rect.height > 0 && 
				   style.visibility !== 'hidden' && 
				   style.display !== 'none';
		}

		function cleanText(text) {
			return (text || '').replace(/\s+/g, ' ').trim();
		}

		function traverse(node, depth) {
			if (!node) return '';
			
			// Обработка текстовых узлов
			if (node.nodeType === Node.TEXT_NODE) {
				const text = cleanText(node.textContent);
				if (text.length > 0) {
					return '  '.repeat(depth) + text + '\n';
				}
				return '';
			}

			// Обработка элементов
			if (node.nodeType === Node.ELEMENT_NODE) {
				const el = node;
				if (!isVisible(el)) return '';

				let output = '';
				let prefix = '  '.repeat(depth);
				const tag = el.tagName.toLowerCase();
				
				if (isInteractive(el)) {
					const aiId = idCounter++;
					el.setAttribute('data-ai-id', aiId);
					
					let extra = '';
					if (tag === 'input' || tag === 'textarea') {
						extra = ' value="' + (el.value || '') + '" placeholder="' + (el.getAttribute('placeholder') || '') + '"';
					}
					if (tag === 'a') {
						extra = ' href="..."'; 
					}

					// !!! ЗДЕСЬ БЫЛА ОШИБКА: заменяем '${...}' на конкатенацию !!!
					output += prefix + '[' + aiId + '] <' + tag + extra + '>\n';
				} else {
					// Опционально: можно выводить структурные теги, если нужно
				}

				// Рекурсия по детям
				for (const child of el.childNodes) {
					output += traverse(child, depth + 1);
				}
				
				return output;
			}
			return '';
		}

		// Чистим старые ID
		document.querySelectorAll('[data-ai-id]').forEach(el => el.removeAttribute('data-ai-id'));

		return traverse(document.body, 0);
	}`

	result, err := m.Page.Evaluate(script)
	if err != nil {
		return nil, fmt.Errorf("js evaluation failed: %w", err)
	}

	treeStr, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("expected string from js, got %T", result)
	}

	title, _ := m.Page.Title()

	return &PageSnapshot{
		URL:   m.Page.URL(),
		Title: title,
		Tree:  treeStr,
	}, nil
}

func (m *Manager) GetSelectorForID(id int) string {
	return fmt.Sprintf("[data-ai-id='%d']", id)
}

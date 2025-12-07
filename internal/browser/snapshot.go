package browser

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/playwright-community/playwright-go"
)

type PageSnapshot struct {
	URL              string
	Title            string
	Tree             string
	ScreenshotBase64 string
}

func (m *Manager) Snapshot() (*PageSnapshot, error) {
	if m == nil || m.Page == nil {
		return nil, fmt.Errorf("page is not initialized")
	}

	// Скрипт для генерации DOM-дерева.
	// ОПТИМИЗАЦИЯ: Игнорируем элементы вне viewport и слишком длинные тексты.
	script := `() => {
       let idCounter = 1;
       const interactiveTags = new Set(['a', 'button', 'input', 'textarea', 'select', 'details', 'summary']);

       // Очистка старых ID, если есть
       document.querySelectorAll('[data-ai-id]').forEach(el => el.removeAttribute('data-ai-id'));

       function cleanText(text) {
          if (!text) return '';
          let res = text.replace(/\s+/g, ' ').trim();
          // Ограничиваем длину текста, чтобы не забивать контекст LLM
          if (res.length > 100) {
             return res.slice(0, 100) + '...';
          }
          return res;
       }

       function isVisible(el) {
          if (!el || !el.getBoundingClientRect) return false;

          const ariaHidden = el.getAttribute('aria-hidden');
          if (ariaHidden === 'true') return false;

          const rect = el.getBoundingClientRect();
          const style = window.getComputedStyle(el);

          // Проверка: находится ли элемент во вьюпорте.
          // Если элемент далеко внизу, он не попадет в дерево, пока агент не проскроллит.
          const inViewport = (
             rect.top < window.innerHeight &&
             rect.bottom > 0 &&
             rect.left < window.innerWidth &&
             rect.right > 0
          );

          return rect.width > 0 && rect.height > 0 &&
             style.visibility !== 'hidden' &&
             style.display !== 'none' &&
             style.opacity !== '0' &&
             inViewport;
       }

       function isInteractive(el) {
          const tag = el.tagName.toLowerCase();
          const role = (el.getAttribute('role') || '').toLowerCase();
          const tabIndex = el.getAttribute('tabindex');

          return interactiveTags.has(tag) ||
             role === 'button' ||
             role === 'link' ||
             role === 'checkbox' ||
             role === 'menuitem' ||
             role === 'tab' ||
             role === 'textbox' ||
             role === 'combobox' ||
             role === 'option' ||
             (tabIndex !== null && tabIndex !== '-1') ||
             el.onclick != null;
       }

       function escapeAttr(value) {
          return value.replace(/"/g, '\\"');
       }

       // Упрощенная проверка контекста
       function getContextFlags(el) {
          let inDialog = false;
          let cur = el;
          while (cur && cur !== document.body) {
             const role = (cur.getAttribute('role') || '').toLowerCase();
             const ariaModal = cur.getAttribute('aria-modal');
             if (role === 'dialog' || role === 'alertdialog' || ariaModal === 'true') {
                inDialog = true;
                break;
             }
             cur = cur.parentElement;
          }
          return { inDialog };
       }

       function getKind(el) {
          const tag = el.tagName.toLowerCase();
          const role = (el.getAttribute('role') || '').toLowerCase();
          const type = (el.getAttribute('type') || '').toLowerCase();

          if (tag === 'button' || role === 'button') return 'button';
          if (tag === 'a' || role === 'link') return 'link';
          if (tag === 'input') {
             if (type === 'checkbox') return 'checkbox';
             if (type === 'radio') return 'radio';
             if (type === 'search') return 'search';
             return 'input';
          }
          return '';
       }

       // Поиск активного модального окна, чтобы начать обход с него (если есть)
       function findActiveModal() {
          const selectors = ['[role="dialog"]', '[role="alertdialog"]', '[aria-modal="true"]', '.modal', '.overlay'];
          const candidates = Array.from(document.querySelectorAll(selectors.join(',')));
          let best = null;
          let bestZ = -Infinity;
          for (const el of candidates) {
             if (!isVisible(el)) continue;
             const style = window.getComputedStyle(el);
             let z = parseInt(style.zIndex || '0', 10);
             if (Number.isNaN(z)) z = 0;
             if (z >= bestZ) {
                bestZ = z;
                best = el;
             }
          }
          return best;
       }

       const activeModal = findActiveModal();
       // Если есть модалка, фокусируемся на ней, иначе на body
       const root = activeModal || document.body;
       let header = activeModal ? "=== ACTIVE DIALOG ===\n" : "";

       function traverse(node, depth) {
          if (!node) return '';
          // Ограничение глубины рекурсии
          if (depth > 20) return '';

          if (node.nodeType === Node.TEXT_NODE) {
             const text = cleanText(node.textContent);
             // Игнорируем очень короткий шум
             if (text.length > 2) {
                return '  '.repeat(depth) + text + '\n';
             }
             return '';
          }

          if (node.nodeType === Node.ELEMENT_NODE) {
             const el = node;
             if (!isVisible(el)) return '';

             let output = '';
             const prefix = '  '.repeat(depth);
             const tag = el.tagName.toLowerCase();

             // Пропускаем "мусорные" теги для экономии
             if (['script', 'style', 'svg', 'path', 'noscript'].includes(tag)) return '';

             if (isInteractive(el)) {
                const aiId = idCounter++;
                el.setAttribute('data-ai-id', String(aiId));

                const parts = ['<' + tag];
                
                // Собираем Label
                let label = cleanText(el.innerText || el.textContent || '');
                if (!label) label = cleanText(el.getAttribute('aria-label') || '');
                if (!label) label = cleanText(el.getAttribute('title') || '');
                if ((tag === 'input' || tag === 'textarea') && !label) {
                   label = cleanText(el.getAttribute('placeholder') || '');
                }

                if (label) parts.push('label="' + escapeAttr(label) + '"');

                const kind = getKind(el);
                if (kind) parts.push('kind="' + kind + '"');

                const ctx = getContextFlags(el);
                if (ctx.inDialog) parts.push('context="dialog"');
                
                // Добавляем value для инпутов
                if (tag === 'input' || tag === 'textarea') {
                    const val = cleanText(el.value);
                    if (val) parts.push('value="' + escapeAttr(val) + '"');
                }

                output += prefix + '[' + aiId + '] ' + parts.join(' ') + '>\n';
             } else {
                // Рендерим только структурные теги или если внутри есть текст
                // Заголовки рендерим всегда с текстом
                if (['h1','h2','h3','h4','h5'].includes(tag)) {
                    const hText = cleanText(el.innerText);
                    output += prefix + '<' + tag + '> ' + hText + '\n';
                } else if (['div', 'p', 'span', 'section', 'li', 'ul', 'form'].includes(tag)) {
                    // Просто контейнер, идем внутрь
                    // Можно вывести тег, чтобы LLM понимала структуру, но без атрибутов
                    // output += prefix + '<' + tag + '>\n'; 
                    // (Для экономии токенов часто лучше опускать div, если он не интерактивный, 
                    // но тогда теряется структура. Оставим вывод тега, если он важен)
                    // В данной версии для макс. экономии НЕ выводим пустые div-ы в строку, 
                    // просто рекурсивно идем внутрь.
                }
             }

             for (const child of el.childNodes) {
                output += traverse(child, depth+1);
             }

             return output;
          }
          return '';
       }

       return header + traverse(root, 0);
    }`

	result, err := m.Page.Evaluate(script)
	if err != nil {
		return nil, fmt.Errorf("js evaluation failed: %w", err)
	}

	treeStr, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("expected string from js, got %T", result)
	}

	// Проверяем наличие маркеров корзины для отладки
	if strings.Contains(treeStr, "Sepete git") {
		fmt.Println("DEBUG: cart button 'Sepete git' IS present in snapshot")
	} else {
		// fmt.Println("DEBUG: cart button 'Sepete git' NOT found in snapshot")
	}

	title, _ := m.Page.Title()

	// Full-page скриншот отключен для экономии токенов и ускорения.
	// Используем JPEG с качеством 70.
	var screenshotB64 string
	if buf, errShot := m.Page.Screenshot(playwright.PageScreenshotOptions{
		FullPage: playwright.Bool(false),
		Type:     playwright.ScreenshotTypeJpeg,
		Quality:  playwright.Int(70),
	}); errShot == nil {
		screenshotB64 = base64.StdEncoding.EncodeToString(buf)
	} else {
		fmt.Printf("Warning: failed to take screenshot: %v\n", errShot)
	}

	return &PageSnapshot{
		URL:              m.Page.URL(),
		Title:            title,
		Tree:             treeStr,
		ScreenshotBase64: screenshotB64,
	}, nil
}

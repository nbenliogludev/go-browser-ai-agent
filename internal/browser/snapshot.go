package browser

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/playwright-community/playwright-go"
)

type PageSnapshot struct {
	URL              string
	Title            string
	Tree             string
	ScreenshotBase64 string
}

func (m *Manager) Snapshot(step int) (*PageSnapshot, error) {
	if m == nil || m.Page == nil {
		return nil, fmt.Errorf("page is not initialized")
	}

	// ИСПРАВЛЕННЫЙ JS (без вложенных backticks, чтобы IDE не ругалась)
	// Добавлен режим ФОКУСИРОВКИ НА МОДАЛКЕ.
	script := `() => {
		let idCounter = 1;
		const MAX_ELEMENTS = 800; 
		let output = [];

		document.querySelectorAll('[data-ai-id]').forEach(el => el.removeAttribute('data-ai-id'));

		// 1. ПРОВЕРКА НА МОДАЛКУ
		// Ищем типичные классы модалок Getir и других сайтов
		const modalSelectors = [
			'[role="dialog"]', 
			'.ReactModal__Content', 
			'.modal-content', 
			'[data-testid="modal"]',
			'.style__ModalContent', // Getir specific
			'.popup'
		];
		
		let rootElement = document.body;
		let isModalMode = false;

		for (const sel of modalSelectors) {
			const modal = document.querySelector(sel);
			if (modal && window.getComputedStyle(modal).display !== 'none') {
				rootElement = modal;
				isModalMode = true;
				output.push("!!! MODAL OPEN DETECTED - FOCUSING ONLY ON POPUP !!!");
				break;
			}
		}

		function cleanText(text) {
			if (!text) return '';
			return text.replace(/\s+/g, ' ').trim().slice(0, 60);
		}

		function isVisible(el) {
			const style = window.getComputedStyle(el);
			if (style.visibility === 'hidden' || style.display === 'none' || style.opacity === '0') return false;
			const rect = el.getBoundingClientRect();
			return rect.width > 0 && rect.height > 0;
		}

		function isInteractive(el, style) {
			const tag = el.tagName.toLowerCase();
			if (['a', 'button', 'select', 'textarea', 'input'].includes(tag)) return true;
			
			const role = el.getAttribute('role');
			if (role === 'button' || role === 'link' || role === 'checkbox') return true;
			
			if (style.cursor === 'pointer') return true;
			if (el.onclick != null) return true;
			
			const cls = (el.getAttribute('class') || '').toLowerCase();
			if (cls.includes('btn') || cls.includes('button') || cls.includes('add') || cls.includes('plus')) return true;

			return false;
		}

		function traverse(node, depth) {
			if (!node || output.length >= MAX_ELEMENTS) return;
			if (node.nodeType !== Node.ELEMENT_NODE) return;

			const el = node;
			const tag = el.tagName.toLowerCase();

			if (['script', 'style', 'noscript', 'meta', 'link', 'br', 'hr'].includes(tag)) return;
			if (!isVisible(el)) return;

			const style = window.getComputedStyle(el);
			const interactive = isInteractive(el, style);
			
			// Текст берем только прямой, чтобы избежать дублей
			let ownText = '';
			if (tag === 'input') {
				ownText = el.value || el.getAttribute('placeholder') || '';
			} else if (el.childNodes.length === 1 && el.childNodes[0].nodeType === Node.TEXT_NODE) {
				ownText = el.textContent;
			} else if (tag.match(/^h[1-6]$/)) {
				ownText = el.textContent;
			} else {
				ownText = el.getAttribute('aria-label') || '';
			}
			ownText = cleanText(ownText);

			let line = '';
			let shouldPrint = false;

			if (interactive) {
				shouldPrint = true;
				// Логика иконок: показываем [ICON] только если это кнопка без текста
				if (!ownText) {
					const hasSvg = el.querySelector('svg') !== null;
					const hasImg = el.querySelector('img') !== null;
					// Если это просто div без svg/img и без текста - это не кнопка, а обертка. Пропускаем.
					if (!hasSvg && !hasImg && tag === 'div' && !el.className.includes('btn')) {
						shouldPrint = false;
					} else {
						ownText = '[ICON]';
					}
				}

				if (shouldPrint) {
					const id = idCounter++;
					el.setAttribute('data-ai-id', String(id));
					
					// Используем конкатенацию для безопасности Go-строк
					line = '[' + id + '] <' + tag + '>';
					if (ownText) line += ' "' + ownText.replace(/"/g, '') + '"';
					
					// Важно: если мы в режиме модалки, добавим пометку
					if (isModalMode) line += ' [MODAL_CONTENT]';
				}
			} else {
				// Неинтерактивный текст (цены, названия)
				if (ownText.length > 2) {
					shouldPrint = true;
					line = ownText;
					if (tag.match(/^h[1-6]$/)) line = '<' + tag + '> ' + line;
				}
			}

			if (shouldPrint) {
				const indent = ' '.repeat(depth); 
				output.push(indent + line);
			}

			// Если мы в модалке, глубину можно сильно не ограничивать
			// Если на главной - ограничиваем
			const nextDepth = shouldPrint ? depth + 1 : depth;
			
			if (nextDepth < 15) {
				const children = el.children;
				for (let i = 0; i < children.length; i++) {
					traverse(children[i], nextDepth);
				}
			}
		}

		traverse(rootElement, 0);
		return output.join('\n');
	}`

	result, err := m.Page.Evaluate(script)
	if err != nil {
		return nil, fmt.Errorf("js evaluation failed: %w", err)
	}
	treeStr, _ := result.(string)

	title, _ := m.Page.Title()

	screenshotParams := playwright.PageScreenshotOptions{
		FullPage: playwright.Bool(false),
		Type:     playwright.ScreenshotTypeJpeg,
		Quality:  playwright.Int(40),
	}

	_ = os.Mkdir("debug", 0755)
	filename := fmt.Sprintf("debug/step_%d.jpg", step)

	var screenshotB64 string
	if buf, errShot := m.Page.Screenshot(screenshotParams); errShot == nil {
		screenshotB64 = base64.StdEncoding.EncodeToString(buf)
		_ = os.WriteFile(filename, buf, 0644)
	}

	return &PageSnapshot{
		URL:              m.Page.URL(),
		Title:            title,
		Tree:             treeStr,
		ScreenshotBase64: screenshotB64,
	}, nil
}

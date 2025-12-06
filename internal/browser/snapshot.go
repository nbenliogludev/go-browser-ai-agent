package browser

import (
	"fmt"
)

type PageSnapshot struct {
	URL   string
	Title string
	Tree  string
}

func (m *Manager) Snapshot() (*PageSnapshot, error) {
	if m == nil || m.Page == nil {
		return nil, fmt.Errorf("page is not initialized")
	}

	script := `() => {
		let idCounter = 1;
		const interactiveTags = new Set(['a', 'button', 'input', 'textarea', 'select', 'details', 'summary']);

		// Удаляем старые data-ai-id
		document.querySelectorAll('[data-ai-id]').forEach(el => el.removeAttribute('data-ai-id'));

		function cleanText(text) {
			return (text || '').replace(/\s+/g, ' ').trim();
		}

		function isVisible(el) {
			if (!el || !el.getBoundingClientRect) return false;
			const rect = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			return rect.width > 0 && rect.height > 0 &&
				style.visibility !== 'hidden' &&
				style.display !== 'none' &&
				style.opacity !== '0';
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

		function getContextFlags(el) {
			let inDialog = false;
			let inOverlay = false;

			let cur = el;
			while (cur && cur !== document.body) {
				const role = cur.getAttribute('role');
				const ariaModal = cur.getAttribute('aria-modal');
				const className = (cur.className && typeof cur.className === 'string') ? cur.className : '';

				if (role === 'dialog' || role === 'alertdialog' || ariaModal === 'true') {
					inDialog = true;
				}
				if (/\bmodal\b|\bdialog\b|\boverlay\b|\bpopup\b/i.test(className)) {
					inOverlay = true;
				}

				cur = cur.parentElement;
			}

			return { inDialog, inOverlay };
		}

		function getKind(el) {
			const tag = el.tagName.toLowerCase();
			const role = (el.getAttribute('role') || '').toLowerCase();
			const type = (el.getAttribute('type') || '').toLowerCase();

			if (tag === 'button') return 'button';
			if (tag === 'a') return 'link';
			if (tag === 'textarea') return 'textarea';
			if (tag === 'select') return 'select';

			if (tag === 'input') {
				if (type === 'checkbox') return 'checkbox';
				if (type === 'radio') return 'radio';
				if (type === 'search') return 'search';
				return 'input';
			}

			if (role === 'button') return 'button';
			if (role === 'link') return 'link';
			if (role === 'combobox') return 'combobox';
			if (role === 'menuitem') return 'menuitem';
			if (role === 'option') return 'option';

			return '';
		}

		function buildLabel(el) {
			const direct = cleanText(el.innerText || el.textContent || '');
			if (direct) return direct;

			const aria = cleanText(el.getAttribute('aria-label') || '');
			if (aria) return aria;

			const title = cleanText(el.getAttribute('title') || '');
			if (title) return title;

			if (el.tagName.toLowerCase() === 'input' || el.tagName.toLowerCase() === 'textarea') {
				const ph = cleanText(el.getAttribute('placeholder') || '');
				if (ph) return ph;
			}

			return '';
		}

		// ---------- поиск активной модалки ----------
		function findActiveModal() {
			const selectors = [
				'[role="dialog"]',
				'[role="alertdialog"]',
				'[aria-modal="true"]',
				'[data-testid*="modal"]',
				'.modal', '.Modal',
				'.overlay', '.Overlay',
				'.popup', '.Popup',
				'.dialog', '.Dialog'
			];
			const candidates = Array.from(
				document.querySelectorAll(selectors.join(','))
			);

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
		const root = activeModal || document.body;

		function traverse(node, depth) {
			if (!node) return '';

			if (node.nodeType === Node.TEXT_NODE) {
				const text = cleanText(node.textContent);
				if (text.length > 0) {
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

				if (isInteractive(el)) {
					const aiId = idCounter++;

					el.setAttribute('data-ai-id', String(aiId));

					const label = buildLabel(el);
					const parts = ['<' + tag];

					if (label) {
						const shortLabel = label.length > 160 ? label.slice(0, 160) + '...' : label;
						parts.push('label="' + escapeAttr(shortLabel) + '"');
					}

					const kind = getKind(el);
					if (kind) {
						parts.push('kind="' + escapeAttr(kind) + '"');
					}

					const ctx = getContextFlags(el);
					if (ctx.inDialog || ctx.inOverlay) {
						parts.push('context="dialog"');
					}

					if (tag === 'input' || tag === 'textarea') {
						const value = cleanText(el.value || '');
						const placeholder = cleanText(el.getAttribute('placeholder') || '');
						if (value) {
							parts.push('value="' + escapeAttr(value) + '"');
						}
						if (placeholder) {
							parts.push('placeholder="' + escapeAttr(placeholder) + '"');
						}
						const inputType = el.getAttribute('type');
						if (inputType) {
							parts.push('type="' + escapeAttr(inputType) + '"');
						}
					}

					if (tag === 'a') {
						const href = el.getAttribute('href');
						if (href) {
							const shortHref = href.length > 160 ? href.slice(0, 160) + '...' : href;
							parts.push('href="' + escapeAttr(shortHref) + '"');
						}
					}

					output += prefix + '[' + aiId + '] ' + parts.join(' ') + '>\n';
				} else {
					// показываем заголовки (в т.ч. заголовок модалки)
					if (['h1','h2','h3','h4','h5'].includes(tag)) {
						const headingText = cleanText(el.innerText || el.textContent || '');
						if (headingText) {
							const shortHeading = headingText.length > 160 ? headingText.slice(0, 160) + '...' : headingText;
							output += prefix + '<' + tag + '> ' + shortHeading + '\n';
						}
					}
				}

				for (const child of el.childNodes) {
					output += traverse(child, depth + 1);
				}

				return output;
			}

			return '';
		}

		return traverse(root, 0);
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

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

	script := `() => {
       let idCounter = 1;
       const interactiveTags = new Set(['a', 'button', 'input', 'textarea', 'select', 'label']);

       document.querySelectorAll('[data-ai-id]').forEach(el => el.removeAttribute('data-ai-id'));

       function cleanText(text) {
          if (!text) return '';
          let res = text.replace(/\s+/g, ' ').trim();
          if (res.length > 50) return res.slice(0, 50) + '...';
          return res;
       }

       // Получаем "полезное" имя класса для контекста (footer, modal, wrapper...)
       function getContextClass(el) {
           if (!el.className || typeof el.className !== 'string') return '';
           // Берем только первые 30 символов класса, чтобы не забивать токены
           // и убираем мусорные классы типа 'sc-123456' (styled components)
           let cls = el.className.split(' ')
               .filter(c => !c.startsWith('sc-') && c.length > 3) // Фильтр мусора
               .slice(0, 3) // Только первые 3 класса
               .join('.');
           
           return cls ? '.' + cls : '';
       }

       function isVisible(el) {
          const style = window.getComputedStyle(el);
          if (style.visibility === 'hidden' || style.display === 'none' || style.opacity === '0') return false;
          const rect = el.getBoundingClientRect();
          if (rect.width === 0 && rect.height === 0 && style.overflow === 'hidden') return false;
          return true; 
       }

       // Viewport: 1.5 экрана
       function isInViewport(el) {
           const rect = el.getBoundingClientRect();
           const windowHeight = window.innerHeight;
           return rect.top < (windowHeight * 1.5) && rect.bottom > 0;
       }

       function isInteractive(el) {
          const tag = el.tagName.toLowerCase();
          if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
          
          if (interactiveTags.has(tag) || el.getAttribute('role') === 'button') return true;
          
          if (el.onclick != null || window.getComputedStyle(el).cursor === 'pointer') return true;
          const className = (el.getAttribute('class') || '').toLowerCase();
          if (className.includes('btn') || className.includes('button')) return true;

          return false;
       }

       // Определяем важность элемента по стилю
       function getImportanceAttributes(el) {
           let attrs = "";
           const style = window.getComputedStyle(el);
           const bg = style.backgroundColor;
           
           // Яркие кнопки
           if (bg && bg !== 'rgba(0, 0, 0, 0)' && bg !== 'transparent' && bg !== 'rgb(255, 255, 255)') {
               attrs += ' style="filled"';
           }
           // Прилипшие элементы (обычно футер с кнопкой купить)
           if (style.position === 'fixed' || style.position === 'sticky') {
               attrs += ' pos="sticky"';
           }
           return attrs;
       }

       function escapeAttr(value) { return value.replace(/"/g, '\\"'); }

       function getLabel(el) {
          // Собираем текст рекурсивно, но не глубоко, чтобы поймать "100 руб + Добавить"
          let text = cleanText(el.innerText);
          if (el.tagName.toLowerCase() === 'input') {
             return cleanText(el.getAttribute('placeholder') || el.value);
          }
          return text;
       }

       function traverse(node, depth) {
          if (!node || depth > 50) return ''; 

          if (node.nodeType === Node.ELEMENT_NODE) {
             const el = node;
             const tag = el.tagName.toLowerCase();
             
             if (['script', 'style', 'svg', 'path', 'noscript', 'meta', 'link', 'img', 'figure', 'picture'].includes(tag)) return '';
             
             if (!isVisible(el)) return '';
             
             const isModal = (el.getAttribute('role') === 'dialog' || 
                              (el.getAttribute('class') || '').includes('modal') || 
                              (el.getAttribute('class') || '').includes('popup'));
             
             if (!isModal && !isInViewport(el)) return ''; 

             let output = '';
             const prefix = '  '.repeat(depth);
             
             if (isModal) output += prefix + '--- MODAL START ---\n';

             const interactive = isInteractive(el);
             
             if (interactive) {
                const aiId = idCounter++;
                el.setAttribute('data-ai-id', String(aiId));
                
                const parts = ['<' + tag];
                
                // Добавляем подсказки важности
                parts.push(getImportanceAttributes(el));

                const label = getLabel(el);
                if (label) parts.push('label="' + escapeAttr(label) + '"');
                if (tag === 'input') parts.push('type="' + (el.getAttribute('type') || 'text') + '"');
                
                output += prefix + '[' + aiId + '] ' + parts.join(' ') + '>\n';
             } else {
                 // --- RESTORED CONTEXT ---
                 // Мы БОЛЬШЕ НЕ СПЛЮЩИВАЕМ (не скрываем) дивы.
                 // Мы показываем их, если у них есть интересный класс или структура.
                 // Чтобы сэкономить токены, мы выводим их упрощенно.
                 
                 const cls = getContextClass(el);
                 const directText = Array.from(el.childNodes).some(
                    child => child.nodeType === Node.TEXT_NODE && child.textContent.trim().length > 2
                 );

                 // Выводим тег, если это заголовок, или если есть класс, или если есть текст
                 if (isModal || directText || /^h[1-6]$/.test(tag) || cls) {
                     let attrs = "";
                     if (cls) attrs += ' class="' + cls + '"';
                     if (directText) attrs += ' text="' + cleanText(el.childNodes[0].textContent) + '"';
                     
                     // Проверка на sticky контейнер (важно для кнопок внизу)
                     const style = window.getComputedStyle(el);
                     if (style.position === 'fixed' || style.position === 'sticky') {
                        attrs += ' pos="sticky"';
                     }

                     output += prefix + '<' + tag + attrs + '>\n';
                 }
             }

             for (const child of el.childNodes) {
                // Всегда увеличиваем отступ, так как мы вернули структуру
                output += traverse(child, depth + 1);
             }
             
             if (isModal) output += prefix + '--- MODAL END ---\n';

             return output;
          }
          return '';
       }

       return traverse(document.body, 0);
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
		Quality:  playwright.Int(70),
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

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

       // --- FIX: VIEWPORT INTERSECTION ---
       // Раньше мы требовали, чтобы элемент был ЦЕЛИКОМ в экране. Это отсекало большие контейнеры.
       // Теперь проверяем ПЕРЕСЕЧЕНИЕ: если элемент хоть немного виден, мы его берем.
       function isInViewport(el) {
           const rect = el.getBoundingClientRect();
           const windowHeight = window.innerHeight || document.documentElement.clientHeight;
           const windowWidth = window.innerWidth || document.documentElement.clientWidth;

           // Элемент виден, если:
           // 1. Его низ ниже верхней границы экрана (rect.bottom > 0)
           // 2. Его верх выше нижней границы экрана (rect.top < windowHeight)
           // 3. Аналогично по горизонтали
           return (
               rect.bottom > 0 &&
               rect.top < windowHeight &&
               rect.right > 0 &&
               rect.left < windowWidth
           );
       }

       function isVisible(el) {
          const style = window.getComputedStyle(el);
          if (style.visibility === 'hidden' || style.display === 'none' || style.opacity === '0') return false;
          const rect = el.getBoundingClientRect();
          if (rect.width === 0 && rect.height === 0 && style.overflow === 'hidden') return false;
          return true; 
       }

       function isNativeInteractive(el) {
           const tag = el.tagName.toLowerCase();
           return interactiveTags.has(tag) || el.getAttribute('role') === 'button';
       }

       function isInteractive(el) {
          const tag = el.tagName.toLowerCase();
          if (tag === 'input' || tag === 'textarea' || tag === 'select') return true;
          
          if (isNativeInteractive(el)) return true;
          
          if (el.onclick != null || window.getComputedStyle(el).cursor === 'pointer') return true;
          const className = (el.getAttribute('class') || '').toLowerCase();
          if (className.includes('btn') || className.includes('button')) return true;

          return false;
       }

       function hasInteractiveChild(el) {
           let found = false;
           function check(node, depth) {
               if (depth > 2 || found) return; // Уменьшили глубину проверки для скорости
               for (const child of node.children) {
                   if (isNativeInteractive(child)) { 
                       found = true; return; 
                   }
                   const style = window.getComputedStyle(child);
                   if (style.cursor === 'pointer') {
                       found = true; return;
                   }
                   check(child, depth + 1);
               }
           }
           check(el, 0);
           return found;
       }

       function escapeAttr(value) { return value.replace(/"/g, '\\"'); }

       function getLabel(el) {
          let text = cleanText(el.innerText || el.textContent);
          if (el.tagName.toLowerCase() === 'input') {
             return cleanText(el.getAttribute('placeholder') || el.value);
          }
          const aria = el.getAttribute('aria-label') || el.getAttribute('title');
          if (aria) return cleanText(aria);
          
          // Для иконок (hh.ru их любит) пробуем найти SVG title или alt картинки
          const svgTitle = el.querySelector('svg title');
          if (svgTitle) return cleanText(svgTitle.textContent);
          
          const img = el.querySelector('img');
          if (img && img.alt) return cleanText(img.alt);
          
          return text;
       }

       function traverse(node, depth) {
          if (!node || depth > 50) return ''; 

          if (node.nodeType === Node.ELEMENT_NODE) {
             const el = node;
             const tag = el.tagName.toLowerCase();
             
             // Убрали img из черного списка, так как иногда кнопка - это просто картинка
             if (['script', 'style', 'noscript', 'meta', 'link'].includes(tag)) return '';
             
             if (!isVisible(el)) return '';
             
             // Проверка на модалку
             const isModal = (el.getAttribute('role') === 'dialog' || 
                              (el.getAttribute('class') || '').includes('modal') || 
                              (el.getAttribute('class') || '').includes('popup') ||
                              (el.getAttribute('data-qa') || '').includes('modal')); // hh.ru specific attribute support

             // Если не модалка и не в зоне видимости - пропускаем
             if (!isModal && !isInViewport(el)) return ''; 

             let output = '';
             const prefix = '  '.repeat(depth);
             
             if (isModal) output += prefix + '--- MODAL START ---\n';

             let interactive = isInteractive(el);
             
             // Если div кликабельный, но внутри есть явная кнопка/ссылка - считаем его оберткой
             if (interactive && !isNativeInteractive(el) && hasInteractiveChild(el)) {
                 interactive = false;
             }

             if (interactive) {
                const aiId = idCounter++;
                el.setAttribute('data-ai-id', String(aiId));
                
                const parts = ['<' + tag];
                const label = getLabel(el);
                if (label) parts.push('label="' + escapeAttr(label) + '"');
                if (tag === 'input') parts.push('type="' + (el.getAttribute('type') || 'text') + '"');
                
                // Добавляем data-qa атрибут, он очень полезен на hh.ru для контекста
                const qa = el.getAttribute('data-qa');
                if (qa) parts.push('qa="' + escapeAttr(qa) + '"');

                output += prefix + '[' + aiId + '] ' + parts.join(' ') + '>\n';

                if (isNativeInteractive(el)) {
                    return output;
                }
             } else {
                 // Flattening: выводим текст только если он есть
                 const directText = Array.from(el.childNodes).some(
                    child => child.nodeType === Node.TEXT_NODE && child.textContent.trim().length > 2
                 );

                 if (isModal || directText || /^h[1-6]$/.test(tag)) {
                     if (directText) {
                         output += prefix + '<text> ' + cleanText(el.innerText) + '\n';
                     } else if (/^h[1-6]$/.test(tag)) {
                         output += prefix + '<' + tag + '> ' + cleanText(el.innerText) + '\n';
                     }
                 }
             }

             for (const child of el.childNodes) {
                const nextDepth = (interactive || isModal || output.trim().length > 0) ? depth + 1 : depth;
                output += traverse(child, nextDepth);
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

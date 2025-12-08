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
       
       // ВАЖНО: Мы больше НЕ удаляем старые ID. 
       // Мы просто перезаписываем их новыми в порядке обхода.
       // Это делает DOM чуть "грязным", но зато элементы не теряются между кадрами.

       const interactiveTags = new Set(['a', 'button', 'input', 'textarea', 'select']);

       function cleanText(text) {
          if (!text) return '';
          return text.replace(/\s+/g, ' ').trim().slice(0, 50); 
       }

       function isVisible(el) {
          const style = window.getComputedStyle(el);
          if (style.visibility === 'hidden' || style.display === 'none' || style.opacity === '0') return false;
          const rect = el.getBoundingClientRect();
          if (rect.width === 0 && rect.height === 0 && style.overflow === 'hidden') return false;
          return true; 
       }

       function isInViewport(el) {
           const rect = el.getBoundingClientRect();
           const windowHeight = window.innerHeight;
           return rect.top < (windowHeight * 1.5) && rect.bottom > -100;
       }

       function isInteractive(el) {
          const tag = el.tagName.toLowerCase();
          if (interactiveTags.has(tag)) return true;
          if (el.getAttribute('role') === 'button') return true;
          if (el.onclick != null || window.getComputedStyle(el).cursor === 'pointer') return true;
          return false;
       }

       function getImportanceAttributes(el) {
           let attrs = "";
           const style = window.getComputedStyle(el);
           const bg = style.backgroundColor;
           
           if (el.getAttribute('kind') === 'primary') {
               attrs += ' priority="high" style="filled"';
           } else if (bg && bg !== 'rgba(0, 0, 0, 0)' && bg !== 'transparent' && bg !== 'rgb(255, 255, 255)') {
               attrs += ' style="filled"';
           }
           if (style.position === 'fixed' || style.position === 'sticky') {
               attrs += ' pos="sticky"';
           }
           return attrs;
       }

       function getLabel(el) {
          let text = cleanText(el.innerText);
          if (el.tagName.toLowerCase() === 'input') {
             return cleanText(el.getAttribute('placeholder') || el.value);
          }
          const aria = el.getAttribute('aria-label');
          return aria ? (text ? text : cleanText(aria)) : text;
       }

       const activeModal = document.querySelector('[role="dialog"], .modal, .popup, [data-testid="modal"]');

       function traverse(node) {
          if (!node) return '';
          
          if (node.nodeType === Node.ELEMENT_NODE) {
             const el = node;
             const tag = el.tagName.toLowerCase();
             
             if (['script', 'style', 'svg', 'path', 'noscript', 'meta', 'link', 'img', 'picture'].includes(tag)) return '';

             // ВАЖНО: больше НЕ отфильтровываем все, что не внутри модалки.
             // Вместо этого используем viewport-фильтр только когда модалки нет.
             if (!isVisible(el)) return '';
             if (!activeModal && !isInViewport(el)) return ''; 

             let output = '';
             
             if (/^h[1-6]$/.test(tag)) {
                 const text = cleanText(el.innerText);
                 if (text) output += '<text class="header">' + text + '</text>\n';
             }

             if (isInteractive(el)) {
                const aiId = idCounter++;
                el.setAttribute('data-ai-id', String(aiId)); // Перезаписываем ID
                
                const parts = ['<' + tag];
                const imp = getImportanceAttributes(el);
                if (imp) parts.push(imp);
                
                // Помечаем элементы, находящиеся в модалке
                if (activeModal && (el === activeModal || activeModal.contains(el))) {
                    parts.push('in_modal="true"');
                }

                const label = getLabel(el);
                if (label) parts.push('label="' + label.replace(/"/g, "'") + '"');
                if (tag === 'input') parts.push('type="' + (el.getAttribute('type') || 'text') + '"');
                
                output += '[' + aiId + '] ' + parts.join(' ') + '>\n';
             }

             for (const child of el.childNodes) {
                output += traverse(child);
             }
             
             if (el === activeModal) {
                 return '--- MODAL START ---\n' + output + '--- MODAL END ---\n';
             }

             return output;
          }
          return '';
       }

       const root = activeModal || document.body;
       return traverse(root);
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

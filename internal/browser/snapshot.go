package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ElementMap хранит BackendNodeID (внутренний ID Chrome) по нашему LLM-ID
type ElementMap map[int]cdp.BackendNodeID

type PageSnapshot struct {
	URL              string
	Title            string
	Tree             string
	ScreenshotBase64 string
	Elements         ElementMap
}

// ------------------ AX TYPES (свои, не из cdproto/accessibility) ------------------

type AXValue struct {
	Value interface{} `json:"value,omitempty"`
}

type AXNode struct {
	NodeID           string            `json:"nodeId"`
	Role             *AXValue          `json:"role,omitempty"`
	Name             *AXValue          `json:"name,omitempty"`
	Value            *AXValue          `json:"value,omitempty"`
	BackendDOMNodeID cdp.BackendNodeID `json:"backendDOMNodeId,omitempty"`
	Ignored          bool              `json:"ignored,omitempty"`
}

type axTreeResult struct {
	Nodes []AXNode `json:"nodes"`
}

// ------------------ SNAPSHOT ------------------

func (m *Manager) Snapshot(step int) (*PageSnapshot, error) {
	var (
		axNodes []AXNode
		axErr   error

		buf        []byte
		url, title string
	)

	err := chromedp.Run(
		m.Ctx,
		chromedp.Location(&url),
		chromedp.Title(&title),

		// Получаем AX-tree через сырой CDP Target (а не Browser!)
		chromedp.ActionFunc(func(ctx context.Context) error {
			exec := chromedp.FromContext(ctx)
			if exec.Target == nil {
				axErr = fmt.Errorf("target executor is nil")
				return nil
			}

			var out axTreeResult
			params := map[string]any{
				"interestingOnly": true, // как в Playwright: только "интересные" узлы
			}

			if err := exec.Target.Execute(ctx, "Accessibility.getFullAXTree", params, &out); err != nil {
				axErr = err
				return nil
			}

			axNodes = out.Nodes
			return nil
		}),

		// Скриншот
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			buf, err = page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatJpeg).
				WithQuality(50).
				Do(ctx)
			return err
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("chromedp snapshot failed: %w", err)
	}

	elements := make(ElementMap)
	idCounter := 1

	var treeStr string
	if axErr == nil && len(axNodes) > 0 {
		treeStr = serializeAXNodes(axNodes, &idCounter, elements)
	} else {
		// Если вдруг AX не доступен – хотя бы DOM fallback, чтобы агент не ослеп
		if axErr != nil {
			log.Printf("⚠️ Accessibility.getFullAXTree failed (%v), fallback to DOM", axErr)
		}
		treeStr = buildDOMFallback(m.Ctx, &idCounter, elements)
	}

	screenshotB64 := ""
	if len(buf) > 0 {
		screenshotB64 = base64.StdEncoding.EncodeToString(buf)
	}

	_ = step // чтобы не ругалась IDE, если не используешь

	return &PageSnapshot{
		URL:              url,
		Title:            title,
		Tree:             treeStr,
		ScreenshotBase64: screenshotB64,
		Elements:         elements,
	}, nil
}

// ------------------ AX SERIALIZATION ------------------

func serializeAXNodes(nodes []AXNode, idCounter *int, elements ElementMap) string {
	if len(nodes) == 0 {
		return ""
	}

	var sb strings.Builder

	for _, node := range nodes {
		if shouldSkipAX(&node) {
			continue
		}

		role := axValueString(node.Role)
		name := axValueString(node.Name)

		isInteractive := isInteractiveRole(role)

		if isInteractive {
			currentID := *idCounter
			*idCounter++

			if node.BackendDOMNodeID != 0 {
				elements[currentID] = node.BackendDOMNodeID
			}

			sb.WriteString(fmt.Sprintf("[%d] ", currentID))
		} else {
			sb.WriteString("- ")
		}

		if role == "" {
			role = "unknown"
		}
		sb.WriteString(fmt.Sprintf("[%s]", role))

		if name != "" {
			cleanName := strings.ReplaceAll(name, "\n", " ")
			if len(cleanName) > 80 {
				cleanName = cleanName[:77] + "..."
			}
			sb.WriteString(fmt.Sprintf(" %q", cleanName))
		}

		if node.Value != nil && node.Value.Value != nil {
			valStr := axValueString(node.Value)
			if valStr != "" {
				sb.WriteString(fmt.Sprintf(" (Val: %s)", valStr))
			}
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

func axValueString(v *AXValue) string {
	if v == nil || v.Value == nil {
		return ""
	}

	switch vv := v.Value.(type) {
	case string:
		return vv
	case float64:
		return fmt.Sprintf("%.2f", vv)
	case bool:
		if vv {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(vv)
	}
}

func shouldSkipAX(node *AXNode) bool {
	role := axValueString(node.Role)
	name := axValueString(node.Name)

	if (role == "genericContainer" || role == "none" || role == "") && name == "" {
		return true
	}
	if node.Ignored {
		return true
	}
	return false
}

func isInteractiveRole(role string) bool {
	switch role {
	case "button", "link", "checkbox", "radioButton",
		"searchBox", "textBox", "comboBox",
		"menuItem", "slider", "switch":
		return true
	default:
		return false
	}
}

// ------------------ DOM FALLBACK НА ВСЯКИЙ СЛУЧАЙ ------------------

func buildDOMFallback(ctx context.Context, idCounter *int, elements ElementMap) string {
	var nodes []*cdp.Node

	err := chromedp.Run(
		ctx,
		chromedp.Nodes(
			`a, button, input, select, textarea, [role], [aria-label]`,
			&nodes,
			chromedp.ByQueryAll,
		),
	)
	if err != nil {
		log.Printf("⚠️ DOM fallback failed: %v", err)
		return ""
	}

	var sb strings.Builder

	for _, n := range nodes {
		currentID := *idCounter
		*idCounter++

		if n.BackendNodeID != 0 {
			elements[currentID] = n.BackendNodeID
		}

		tag := strings.ToLower(n.NodeName)
		sb.WriteString(fmt.Sprintf("[%d] <%s>\n", currentID, tag))
	}

	return sb.String()
}

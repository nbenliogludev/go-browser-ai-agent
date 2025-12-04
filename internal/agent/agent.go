package agent

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

type Agent struct {
	browser *browser.Manager
	llm     llm.Client
}

func NewAgent(b *browser.Manager, c llm.Client) *Agent {
	return &Agent{
		browser: b,
		llm:     c,
	}
}

func (a *Agent) Run(task string, maxSteps int) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("Запускаю агента с задачей: %q (maxSteps=%d)\n", task, maxSteps)

	var (
		prevAction    *llm.Action
		prevURL       string
		repeatCounter int
	)

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n=== Шаг %d ===\n", step)

		snapshot, err := a.browser.Snapshot(50)
		if err != nil {
			return fmt.Errorf("snapshot error: %w", err)
		}

		currentURL := snapshot.URL

		fmt.Printf("Текущая страница: %s (%s), элементов: %d\n",
			snapshot.Title, snapshot.URL, len(snapshot.Elements))

		decision, err := a.llm.DecideAction(llm.DecisionInput{
			Task:     task,
			Snapshot: snapshot,
		})
		if err != nil {
			return fmt.Errorf("LLM decision error: %w", err)
		}

		fmt.Printf("Thought: %s\n", decision.Thought)
		fmt.Printf("Action: %+v\n", decision.Action)

		// ---------- детектор зацикливания на вводе текста ----------
		if prevAction != nil &&
			decision.Action.Type == llm.ActionTypeText &&
			prevAction.Type == llm.ActionTypeText &&
			decision.Action.Text == prevAction.Text && // тот же текст
			currentURL == prevURL { // на той же странице
			repeatCounter++
		} else {
			repeatCounter = 0
		}

		// если уже второй раз подряд печатаем одно и то же на той же странице —
		// считаем, что агент застрял и пробуем отправить форму через Enter
		if repeatCounter >= 1 {
			fmt.Println("Похоже, агент застрял на вводе текста — пробую отправить форму через Enter.")

			target, ok := findElementByID(snapshot, decision.Action.TargetID)
			if ok && (target.Tag == "input" || target.Tag == "textarea") {
				fmt.Printf("Жму Enter в поле selector=%s\n", target.Selector)
				if err := a.browser.Page.Press(target.Selector, "Enter"); err != nil {
					return fmt.Errorf("failed to press Enter: %w", err)
				}
				a.browser.Page.WaitForTimeout(1500)

				// обновляем состояние и переходим к следующему шагу
				prevAction = &decision.Action
				prevURL = currentURL
				continue
			}
		}
		// ---------- конец детектора зацикливания ----------

		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("Агент считает, что задача выполнена (ActionFinish). Останавливаю цикл.")
			return nil
		}

		if err := a.executeWithSecurity(reader, snapshot, decision.Action); err != nil {
			return fmt.Errorf("failed to execute action: %w", err)
		}

		prevAction = &decision.Action
		prevURL = currentURL

		a.browser.Page.WaitForTimeout(1500)
	}

	return fmt.Errorf("max steps (%d) reached before task completion", maxSteps)
}

func (a *Agent) executeWithSecurity(reader *bufio.Reader, snapshot *browser.PageSnapshot, action llm.Action) error {
	switch action.Type {
	case llm.ActionClick, llm.ActionTypeText:
		target, ok := findElementByID(snapshot, action.TargetID)
		if !ok {
			return fmt.Errorf("no element with id %q in snapshot", action.TargetID)
		}

		if isDestructiveText(target.Text) {
			prompt := fmt.Sprintf(
				"Security check: агент хочет нажать на элемент с текстом %q (selector=%s). Продолжить? [y/N]: ",
				target.Text, target.Selector,
			)
			if !askForConfirmation(reader, prompt) {
				fmt.Println("Пользователь отклонил действие — пропускаю этот шаг.")
				return nil
			}
		}

		switch action.Type {
		case llm.ActionClick:
			fmt.Printf("Выполняю click по selector=%s\n", target.Selector)
			err := a.browser.Page.Click(target.Selector)
			return err

		case llm.ActionTypeText:
			if action.Text == "" {
				return fmt.Errorf("no text provided for type action")
			}

			// generic правило: печатаем только в input/textarea
			if target.Tag != "input" && target.Tag != "textarea" {
				return fmt.Errorf(
					"cannot type into non-input element: tag=%s role=%s selector=%s",
					target.Tag, target.Role, target.Selector,
				)
			}

			fmt.Printf("Выполняю type(%q) в selector=%s (tag=%s role=%s)\n",
				action.Text, target.Selector, target.Tag, target.Role)

			err := a.browser.Page.Fill(target.Selector, action.Text)
			return err
		}

	case llm.ActionNavigate:
		if action.URL == "" {
			return fmt.Errorf("empty URL for navigate action")
		}

		if isDestructiveURL(action.URL) {
			prompt := fmt.Sprintf(
				"Security check: агент хочет перейти по URL %q. Продолжить? [y/N]: ",
				action.URL,
			)
			if !askForConfirmation(reader, prompt) {
				fmt.Println("Пользователь отклонил переход — пропускаю этот шаг.")
				return nil
			}
		}

		fmt.Printf("Выполняю навигацию на %s\n", action.URL)
		_, err := a.browser.Page.Goto(action.URL)
		return err

	default:
		fmt.Printf("Неизвестный тип действия: %q — пропускаю.\n", action.Type)
		return nil
	}

	return nil
}

func findElementByID(snapshot *browser.PageSnapshot, id string) (*browser.ElementInfo, bool) {
	for i := range snapshot.Elements {
		if snapshot.Elements[i].ID == id {
			return &snapshot.Elements[i], true
		}
	}
	return nil, false
}

func isDestructiveText(text string) bool {
	lower := strings.ToLower(text)
	dangerous := []string{
		"delete",
		"удалить",
		"remove",
		"оплатить",
		"pay",
		"buy",
		"заказать",
		"checkout",
		"удаление",
	}
	for _, word := range dangerous {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

func isDestructiveURL(url string) bool {
	lower := strings.ToLower(url)
	keywords := []string{
		"/checkout",
		"/pay",
		"/payment",
	}
	for _, word := range keywords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

func askForConfirmation(reader *bufio.Reader, prompt string) bool {
	fmt.Print(prompt)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes" || line == "д" || line == "да"
}

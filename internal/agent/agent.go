package agent

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/planner"
)

type Agent struct {
	browser *browser.Manager
	llm     llm.Client

	// План, построенный high-level планировщиком для одной задачи.
	plan *planner.Plan

	// Простая память в рамках одного запуска агента:
	// сюда складываем текст, прочитанный через read_content.
	observations []string
}

func NewAgent(b *browser.Manager, c llm.Client) *Agent {
	return &Agent{
		browser:      b,
		llm:          c,
		plan:         nil,
		observations: nil,
	}
}

func (a *Agent) Run(task string, maxSteps int) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("Запускаю агента с задачей: %q (maxSteps=%d)\n", task, maxSteps)

	// каждый запуск — новая история
	a.observations = nil

	// 1) Пытаемся построить high-level план через отдельного планировщика.
	plan, err := planner.BuildPlan(task)
	if err != nil {
		fmt.Printf("Планировщик не смог построить план: %v\nРаботаем без явного плана.\n", err)
	} else {
		a.plan = plan
		fmt.Println("План от планировщика:")
		for _, s := range plan.Steps {
			fmt.Printf("- [%d] %s (критерий успеха: %s)\n", s.ID, s.Description, s.SuccessCriteria)
		}
	}

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n=== Шаг %d ===\n", step)

		snapshot, err := a.browser.Snapshot(50)
		if err != nil {
			return fmt.Errorf("snapshot error: %w", err)
		}

		fmt.Printf("Текущая страница: %s (%s), элементов: %d\n",
			snapshot.Title, snapshot.URL, len(snapshot.Elements))

		// 2) Собираем "расширенную" задачу:
		// - исходный пользовательский запрос
		// - план (если есть)
		// - предыдущие наблюдения (прочитанный текст)
		effectiveTask := buildTaskWithHistory(task, a.plan, a.observations)

		decision, err := a.llm.DecideAction(llm.DecisionInput{
			Task:     effectiveTask,
			Snapshot: snapshot,
		})
		if err != nil {
			return fmt.Errorf("LLM decision error: %w", err)
		}

		fmt.Printf("Thought: %s\n", decision.Thought)
		fmt.Printf("Action: %+v\n", decision.Action)

		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("Агент считает, что задача выполнена (ActionFinish). Останавливаю цикл.")
			return nil
		}

		if err := a.executeWithSecurity(reader, snapshot, decision.Action); err != nil {
			return fmt.Errorf("failed to execute action: %w", err)
		}

		// Даём странице немного времени обновиться.
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
			if err := a.browser.Page.Click(target.Selector); err != nil {
				return err
			}
			return nil

		case llm.ActionTypeText:
			if action.Text == "" {
				return fmt.Errorf("no text provided for type action")
			}

			if target.Tag != "input" && target.Tag != "textarea" {
				return fmt.Errorf("cannot type into non-textbox element: tag=%s role=%s selector=%s",
					target.Tag, target.Role, target.Selector)
			}

			fmt.Printf("Выполняю type(%q) в selector=%s (tag=%s role=%s)\n",
				action.Text, target.Selector, target.Tag, target.Role)

			if err := a.browser.Page.Fill(target.Selector, action.Text); err != nil {
				return err
			}
			return nil
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
		if _, err := a.browser.Page.Goto(action.URL); err != nil {
			return err
		}
		return nil

	case llm.ActionReadContent:
		target, ok := findElementByID(snapshot, action.TargetID)
		if !ok {
			return fmt.Errorf("no element with id %q in snapshot", action.TargetID)
		}

		maxChars := action.MaxChars
		if maxChars <= 0 || maxChars > 4000 {
			maxChars = 1500
		}

		fmt.Printf("ReadContent: читаю текст вокруг элемента %q (selector=%s, maxChars=%d)\n",
			target.Text, target.Selector, maxChars)

		content, err := a.browser.ReadContent(target.Selector, maxChars)
		if err != nil {
			return fmt.Errorf("read_content failed: %w", err)
		}
		if content == "" {
			fmt.Println("ReadContent: пустой текст — возможно, элемент декоративный.")
			return nil
		}

		preview := content
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}

		fmt.Printf("Наблюдение (усечено до 500 символов):\n%s\n", preview)
		a.observations = append(a.observations, content)
		return nil

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

func buildTaskWithHistory(baseTask string, plan *planner.Plan, observations []string) string {
	var sb strings.Builder

	sb.WriteString("GLOBAL TASK:\n")
	sb.WriteString(baseTask)
	sb.WriteString("\n")

	if plan != nil && len(plan.Steps) > 0 {
		sb.WriteString("\nPLAN (high-level steps):\n")
		for _, s := range plan.Steps {
			sb.WriteString(fmt.Sprintf("%d) %s\n   success_criteria: %s\n", s.ID, s.Description, s.SuccessCriteria))
		}
	}

	if len(observations) > 0 {
		const maxHistory = 5
		start := 0
		if len(observations) > maxHistory {
			start = len(observations) - maxHistory
		}

		sb.WriteString("\nPREVIOUS OBSERVATIONS (last UI reads):\n")
		for i, obs := range observations[start:] {
			trimmed := obs
			if len(trimmed) > 400 {
				trimmed = trimmed[:400] + "..."
			}
			sb.WriteString(fmt.Sprintf("%d) %s\n", i+1, trimmed))
		}
	}

	sb.WriteString("\nYou are a browser agent. Use the PLAN above as guidance, but choose a single concrete action (navigate / click / type / read_content / finish) appropriate for the CURRENT page.\n")

	return sb.String()
}

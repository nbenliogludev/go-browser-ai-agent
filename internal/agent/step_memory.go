package agent

import (
	"fmt"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

// StepMemory хранит краткую историю шагов и детектит циклы
// как по одному действию, так и по повторяющимся паттернам действий.
type StepMemory struct {
	lines    []string
	maxLines int

	// для простого повторения одного и того же действия
	lastActionKey string
	repeatCount   int
	loopThreshold int

	// для паттернов (например, "click-250 -> click-5" повторяется)
	recentKeys    []string
	maxRecent     int
	patternLen    int
	patternCounts map[string]int

	// признак того, что защита от цикла уже хоть раз срабатывала
	loopTriggered bool
}

func NewStepMemory(maxLines, loopThreshold int) *StepMemory {
	if maxLines <= 0 {
		maxLines = 5
	}
	if loopThreshold <= 1 {
		loopThreshold = 2
	}
	return &StepMemory{
		maxLines:      maxLines,
		loopThreshold: loopThreshold,
		maxRecent:     10,
		patternLen:    2, // паттерн из двух действий (A -> B)
		patternCounts: make(map[string]int),
	}
}

func (m *StepMemory) makeKey(url string, action llm.Action) string {
	// тип + URL + target_id — достаточно, чтобы понять,
	// что жмём одну и ту же кнопку на той же странице.
	return fmt.Sprintf("%s|%s|%d", action.Type, url, action.TargetID)
}

// Add — добавить успешно выполненное действие в историю
// и обновить счётчики повторов и паттернов.
func (m *StepMemory) Add(step int, url string, action llm.Action) {
	line := fmt.Sprintf(
		"step=%d url=%s action=%s target=%d text=%q",
		step, url, action.Type, action.TargetID, action.Text,
	)
	m.lines = append(m.lines, line)
	if len(m.lines) > m.maxLines {
		m.lines = m.lines[len(m.lines)-m.maxLines:]
	}

	key := m.makeKey(url, action)

	// простой счётчик "одно и то же действие подряд"
	if key == m.lastActionKey {
		m.repeatCount++
	} else {
		m.lastActionKey = key
		m.repeatCount = 1
	}

	// добавляем в последовательность последних действий
	m.recentKeys = append(m.recentKeys, key)
	if len(m.recentKeys) > m.maxRecent {
		m.recentKeys = m.recentKeys[len(m.recentKeys)-m.maxRecent:]
	}

	// обновляем счётчики паттернов (последовательностей длины patternLen)
	if m.patternLen > 1 && len(m.recentKeys) >= m.patternLen {
		start := len(m.recentKeys) - m.patternLen
		seq := m.recentKeys[start:]
		pattern := strings.Join(seq, "->")
		m.patternCounts[pattern]++
	}
}

// ShouldBlock возвращает (true, reason), если текущее действие
// нужно заблокировать из-за цикла.
func (m *StepMemory) ShouldBlock(url string, action llm.Action) (bool, string) {
	key := m.makeKey(url, action)

	// 1) Тот же самый action слишком много раз подряд
	if m.loopThreshold > 0 && key == m.lastActionKey && m.repeatCount >= m.loopThreshold {
		reason := fmt.Sprintf(
			"SYSTEM NOTE: The same action (%s) has already been executed %d times in a row. "+
				"Do NOT repeat it again. Choose a different action or finish if the goal is already achieved.",
			key, m.repeatCount,
		)
		return true, reason
	}

	// 2) Повторяется паттерн из patternLen действий (например, A->B, A->B, A->B)
	if m.patternLen > 1 && len(m.recentKeys) >= m.patternLen-1 {
		// берём (patternLen-1) последних ключей и добавляем текущий — получаем паттерн
		start := len(m.recentKeys) - (m.patternLen - 1)
		if start < 0 {
			start = 0
		}
		seq := append([]string{}, m.recentKeys[start:]...)
		seq = append(seq, key)

		if len(seq) == m.patternLen {
			pattern := strings.Join(seq, "->")
			if count, ok := m.patternCounts[pattern]; ok && count >= 1 {
				reason := fmt.Sprintf(
					"SYSTEM NOTE: The sequence of %d actions (%s) has already occurred before. "+
						"Do NOT repeat this pattern. Try a different action (for example, moving to the next stage of the flow or finishing).",
					m.patternLen, pattern,
				)
				return true, reason
			}
		}
	}

	return false, ""
}

// AddSystemNote — добавить системную заметку в историю (для LLM).
func (m *StepMemory) AddSystemNote(note string) {
	if strings.TrimSpace(note) == "" {
		return
	}
	m.lines = append(m.lines, note)
	if len(m.lines) > m.maxLines {
		m.lines = m.lines[len(m.lines)-m.maxLines:]
	}
}

// HistoryLines — история шагов + системные заметки.
func (m *StepMemory) HistoryLines() []string {
	if len(m.lines) == 0 {
		return nil
	}
	out := make([]string, len(m.lines))
	copy(out, m.lines)
	return out
}

// HistoryString — удобный вариант для старого Agent (одна строка).
func (m *StepMemory) HistoryString() string {
	lines := m.HistoryLines()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// MarkLoopTriggered — пометить, что защита от цикла уже срабатывала.
func (m *StepMemory) MarkLoopTriggered() {
	m.loopTriggered = true
}

// LoopTriggered — вернуть, срабатывала ли уже защита от цикла.
func (m *StepMemory) LoopTriggered() bool {
	return m.loopTriggered
}

package agent

import (
	"fmt"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

type StepMemory struct {
	lines    []string
	maxLines int

	fullLines []string

	lastActionKey string
	repeatCount   int
	loopThreshold int

	recentKeys    []string
	maxRecent     int
	patternLen    int
	patternCounts map[string]int

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
		patternLen:    2,
		patternCounts: make(map[string]int),
	}
}

func (m *StepMemory) makeKey(url string, action llm.Action) string {
	return fmt.Sprintf("%s|%s|%d", action.Type, url, action.TargetID)
}

func (m *StepMemory) Add(step int, url string, action llm.Action) {
	line := fmt.Sprintf(
		"step=%d url=%s action=%s target=%d text=%q",
		step, url, action.Type, action.TargetID, action.Text,
	)

	m.fullLines = append(m.fullLines, line)

	m.lines = append(m.lines, line)
	if len(m.lines) > m.maxLines {
		m.lines = m.lines[len(m.lines)-m.maxLines:]
	}

	key := m.makeKey(url, action)

	if key == m.lastActionKey {
		m.repeatCount++
	} else {
		m.lastActionKey = key
		m.repeatCount = 1
	}

	m.recentKeys = append(m.recentKeys, key)
	if len(m.recentKeys) > m.maxRecent {
		m.recentKeys = m.recentKeys[len(m.recentKeys)-m.maxRecent:]
	}

	if m.patternLen > 1 && len(m.recentKeys) >= m.patternLen {
		start := len(m.recentKeys) - m.patternLen
		seq := m.recentKeys[start:]
		pattern := strings.Join(seq, "->")
		m.patternCounts[pattern]++
	}
}

func (m *StepMemory) ShouldBlock(url string, action llm.Action) (bool, string) {
	key := m.makeKey(url, action)

	if m.loopThreshold > 0 && key == m.lastActionKey && m.repeatCount >= m.loopThreshold {
		reason := fmt.Sprintf(
			"SYSTEM NOTE: The same action (%s) has already been executed %d times in a row. "+
				"Do NOT repeat it again. Choose a different action or finish if the goal is already achieved.",
			key, m.repeatCount,
		)
		return true, reason
	}

	if m.patternLen > 1 && len(m.recentKeys) >= m.patternLen-1 {
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

func (m *StepMemory) AddSystemNote(note string) {
	note = strings.TrimSpace(note)
	if note == "" {
		return
	}

	m.fullLines = append(m.fullLines, note)

	m.lines = append(m.lines, note)
	if len(m.lines) > m.maxLines {
		m.lines = m.lines[len(m.lines)-m.maxLines:]
	}
}

func (m *StepMemory) HistoryLines() []string {
	if len(m.lines) == 0 {
		return nil
	}
	out := make([]string, len(m.lines))
	copy(out, m.lines)
	return out
}

func (m *StepMemory) HistoryString() string {
	lines := m.HistoryLines()
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (m *StepMemory) FullHistory() []string {
	if len(m.fullLines) == 0 {
		return nil
	}
	out := make([]string, len(m.fullLines))
	copy(out, m.fullLines)
	return out
}

func (m *StepMemory) MarkLoopTriggered() {
	m.loopTriggered = true
}

func (m *StepMemory) LoopTriggered() bool {
	return m.loopTriggered
}

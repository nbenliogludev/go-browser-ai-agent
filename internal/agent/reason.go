package agent

func humanizeReason(reason string) string {
	switch reason {
	case "task finished":
		return "model explicitly finished the task"
	case "max steps reached":
		return "step limit reached"
	case "interrupted by user (Ctrl+C)":
		return "execution was interrupted by user (Ctrl+C)"
	case "llm error":
		return "LLM client error"
	case "snapshot error":
		return "page snapshot error"
	default:
		return reason
	}
}

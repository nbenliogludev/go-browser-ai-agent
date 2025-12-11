# Go Browser AI Agent

An intelligent browser automation agent that navigates websites using natural language instructions. The agent uses vision-based AI to understand web pages and autonomously complete tasks across any website.

## 🎯 Overview

This AI agent can perform complex web interactions by:
- Understanding natural language task descriptions
- Analyzing web pages visually using GPT-4 Vision
- Making intelligent decisions about which elements to interact with
- Executing actions autonomously until task completion
- Generating detailed execution reports

**Demo:** [Watch the agent in action](https://youtu.be/E6QdEMtpCGI)

## ✨ Key Features

- **Vision-Based Understanding**: Uses GPT-4 Vision API to "see" and comprehend web pages like a human
- **Generic Task Execution**: Works across different websites without hardcoded logic (e-commerce, social media, streaming services, etc.)
- **Intelligent Navigation**: Builds accessibility trees and analyzes page structure for robust element detection
- **Loop Prevention**: Automatically detects and breaks out of repetitive action patterns
- **Detailed Reporting**: Generates step-by-step execution summaries
- **Real-time Debugging**: Provides verbose logging of AI thoughts and actions

## 🚀 How It Works

1. **Task Input**: User provides a starting URL and natural language task description
2. **Page Snapshot**: Agent captures:
   - Current URL and page title
   - Accessibility tree with interactive elements
   - Screenshot of the visible page area
3. **AI Decision**: Snapshot sent to LLM which decides next action (click, type, scroll, finish)
4. **Action Execution**: Agent performs action via Chrome DevTools Protocol
5. **Loop Detection**: System prevents repetitive action sequences
6. **Iteration**: Process repeats until task completion or max steps reached
7. **Report Generation**: Summary of steps taken and final state

## 📋 Prerequisites

- **Go**: Version 1.21 or higher
- **OpenAI API Key**: With GPT-4 Vision access
- **Chrome/Chromium**: Browser will be automatically managed by chromedp

## 🔧 Installation

1. **Clone the repository:**
```bash
git clone https://github.com/nbenliogludev/go-browser-ai-agent.git
cd go-browser-ai-agent
```

2. **Install dependencies:**
```bash
go mod download
```

3. **Set up OpenAI API key:**
```bash
export OPENAI_API_KEY="your-api-key-here"
```

## 💻 Usage

### Basic Usage

Run the agent:
```bash
go run cmd/agent-cli/main.go
```

You'll be prompted for:
1. **Starting URL** (press Enter for default: https://example.com)
2. **Task description** in natural language

### Example Tasks

**Food Delivery:**
```
URL: https://getir.com/yemek/
Task: orta boy pizza margarita sepete ekle sonra sepete geç
(Find medium margherita pizza, add to cart, then go to cart)
```

**E-commerce:**
```
URL: https://amazon.com
Task: Find wireless headphones under $50 and add the top-rated one to cart
```

**Social Media:**
```
URL: https://linkedin.com
Task: Search for Senior Go Developer jobs in San Francisco
```

**Streaming:**
```
URL: https://netflix.com
Task: Find science fiction movies and add the first one to my list
```

**Job Search:**
```
URL: https://indeed.com
Task: Search for remote software engineer positions and apply filters for full-time
```

## 📁 Project Structure

```
go-browser-ai-agent/
├── cmd/
│   └── agent-cli/
│       └── main.go              # CLI entry point
├── internal/
│   ├── agent/
│   │   ├── agent.go             # Core agent logic and step execution
│   │   ├── env.go               # Environment configuration
│   │   └── memory.go            # Memory management
│   ├── browser/
│   │   ├── manager.go           # Browser automation via CDP
│   │   └── snapshot.go          # Page snapshot creation
│   └── llm/
│       ├── openai_client.go     # OpenAI Vision API client
│       └── types.go             # LLM type definitions
├── .playwright_data/            # Browser data directory (auto-created)
├── go.mod
├── go.sum
└── README.md
```

## 🛠️ Tech Stack

- **Go**: Core programming language
- **chromedp**: Chrome DevTools Protocol for browser control and automation
- **OpenAI GPT-4 Vision**: Visual understanding and decision making
- **Accessibility Tree (AX)**: Robust interactive element detection

## ⚙️ Configuration

Key parameters (modifiable in `internal/agent/agent.go`):

```go
maxSteps := 40                    // Maximum actions before stopping
loopDetectionWindow := 2          // Recent actions to check for patterns
viewport := [2]int{1280, 720}     // Browser window size
```

## 🐛 Troubleshooting

### Common Issues

**OpenAI Vision API errors (400/401):**
- Verify your API key has GPT-4 Vision access
- Check API key is correctly exported: `echo $OPENAI_API_KEY`
- Ensure you have sufficient API credits

**Rate limit errors (429):**
- Agent automatically retries with exponential backoff
- Reduce task complexity or break into smaller steps
- Consider upgrading OpenAI API tier

**Browser launch failures:**
- Ensure Chrome/Chromium is installed on your system
- chromedp will use the system Chrome installation
- Check that Chrome is not already running with the same profile


## 📊 Output & Reporting

The agent provides:
- **Real-time logs**: AI thoughts and action decisions
- **Debug information**: Element selections, screenshots taken
- **Step summary**: Numbered sequence of all actions
- **Final report**: Task completion status and end state

Example output:
```
--- STEP 1 ---
🤖 THOUGHT: I can see a search bar at the top. I'll type the search query.
⚡ ACTION: type [id=5] text="wireless headphones"

--- STEP 2 ---
🤖 THOUGHT: Search results are visible. I'll click the first product.
⚡ ACTION: click [id=12]
...
```


## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

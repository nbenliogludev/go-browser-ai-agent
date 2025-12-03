package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
)

func main() {
	fmt.Println("Starting AI browser agent (browser bootstrap)...")

	mgr, err := browser.NewManager()
	if err != nil {
		log.Fatalf("failed to start browser manager: %v", err)
	}
	defer mgr.Close()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Введите URL (пусто = https://example.com): ")
	raw, _ := reader.ReadString('\n')
	url := strings.TrimSpace(raw)
	if url == "" {
		url = "https://example.com"
	}

	_, err = mgr.Page.Goto(url)
	if err != nil {
		log.Fatalf("could not navigate to %s: %v", url, err)
	}

	fmt.Println("Браузер открыт. Сессия persistent (логины/куки сохранятся между запусками).")
	fmt.Println("Нажмите Enter в терминале, чтобы завершить работу агента и закрыть браузер.")
	_, _ = reader.ReadString('\n')
}

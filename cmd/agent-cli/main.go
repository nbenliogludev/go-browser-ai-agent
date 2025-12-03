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
	fmt.Println("Starting AI browser agent (DOM snapshot demo)...")

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

	fmt.Println("Страница загружена, извлекаю DOM-снапшот...")

	snapshot, err := mgr.Snapshot(50)
	if err != nil {
		log.Fatalf("could not snapshot page: %v", err)
	}

	fmt.Printf("URL:   %s\n", snapshot.URL)
	fmt.Printf("Title: %s\n", snapshot.Title)
	fmt.Printf("Elements (up to %d):\n", len(snapshot.Elements))

	for _, el := range snapshot.Elements {
		fmt.Printf("- %s [%s] id=%s selector=%s text=%q\n",
			el.Tag, el.Role, el.ID, el.Selector, el.Text)
	}

	fmt.Println("\nНажмите Enter, чтобы закрыть браузер...")
	_, _ = reader.ReadString('\n')
}

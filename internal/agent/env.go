package agent

import (
	"fmt"
	"net/url"
	"strings"
)

// BuildTaskWithEnvironment добавляет к пользовательской задаче контекст про домен и стартовый путь.
func BuildTaskWithEnvironment(rawTask, startURL string) string {
	u, err := url.Parse(startURL)
	if err != nil || u.Host == "" {
		return rawTask
	}

	host := strings.ToLower(u.Host)
	path := strings.TrimRight(u.Path, "/")

	var pathNote string
	if path != "" && path != "/" {
		pathNote = fmt.Sprintf(`
Начальный путь на сайте: %s.
Старайся по возможности оставаться в разделе, URL которого начинается с этого пути.
Не переходи в другие крупные разделы сайта (другие корневые пути), если пользователь
явно об этом не просил, особенно через глобальное меню в шапке.`,
			path,
		)
	}

	return fmt.Sprintf(
		`Ты работаешь на сайте %s.
Начальная страница: %s.%s

Не переходи на другие домены и не открывай внешние поисковые системы.
Задача пользователя: %s`,
		host, startURL, pathNote, rawTask,
	)
}

package tgbot

import "fmt"

func (b *Bot) tr(tgID int64, key string, args ...any) string {
	lang := b.lang(tgID)
	dict := botRU
	if lang == "en" {
		dict = botEN
	}
	s, ok := dict[key]
	if !ok {
		s = botRU[key]
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

func helpText(lang string) string {
	if lang == "en" {
		return helpEN
	}
	return helpRU
}

var botRU = map[string]string{
	"not_authorized":  "⛔ Доступ закрыт.\nПопросите администратора привязать ваш Telegram ID <code>%d</code> в «Пользователи» или выполнить /add %d.",
	"usage_search":    "Использование: /search <запрос>",
	"usage_id":        "Использование: /id <номер>",
	"usage_add":       "Использование: /add <telegram_user_id>",
	"empty_query":     "Пустой запрос.",
	"search_fail":     "Не удалось выполнить поиск.",
	"no_results":      "Ничего не найдено.",
	"found_header":    "🔍 Найдено %d книг. Страница %d/%d",
	"finished_mark":   "✅ прочитано",
	"prev":            "Назад",
	"next":            "Вперед",
	"back":            "Назад",
	"cancel":          "Отмена",
	"session_expired": "Результаты устарели — выполните поиск снова.",
	"book_missing":    "Книга не найдена.",
	"series_label":    "Серия:",
	"genre_label":     "Жанр:",
	"lang_label":      "Язык:",
	"pick_format":     "Выберите формат для скачивания:\n✅ — сразу · 🔄 — конвертация",
	"compact":         "(текст)",
	"preparing":       "⏳ Готовлю файл…",
	"download_fail":   "Не удалось отправить файл.",
	"no_library":      "Каталог файлов не настроен на сервере.",
	"stats":           "📚 В библиотеке <b>%d</b> книг.",
	"lang_set":        "Язык интерфейса обновлён.",
	"admin_only":      "Только для администраторов.",
	"add_ok":          "✅ Пользователь <code>%s</code> авторизован (Telegram ID %d).",
	"add_fail":        "Не удалось авторизовать.",
	"upload_soon":     "Загрузка через бота пока не поддерживается — используйте веб-интерфейс.",
	"unknown_cmd":     "Неизвестная команда. /help — справка.",
}

var botEN = map[string]string{
	"not_authorized":  "⛔ Access denied.\nAsk an admin to link your Telegram ID <code>%d</code> in Users, or run /add %d.",
	"usage_search":    "Usage: /search <query>",
	"usage_id":        "Usage: /id <number>",
	"usage_add":       "Usage: /add <telegram_user_id>",
	"empty_query":     "Empty query.",
	"search_fail":     "Search failed.",
	"no_results":      "No books found.",
	"found_header":    "🔍 Found %d books. Page %d/%d",
	"finished_mark":   "✅ read",
	"prev":            "Prev",
	"next":            "Next",
	"back":            "Back",
	"cancel":          "Cancel",
	"session_expired": "Results expired — search again.",
	"book_missing":    "Book not found.",
	"series_label":    "Series:",
	"genre_label":     "Genre:",
	"lang_label":      "Language:",
	"pick_format":     "Choose a download format:\n✅ — ready · 🔄 — conversion",
	"compact":         "(text)",
	"preparing":       "⏳ Preparing file…",
	"download_fail":   "Could not send the file.",
	"no_library":      "Book files are not configured on the server.",
	"stats":           "📚 Library has <b>%d</b> books.",
	"lang_set":        "Interface language updated.",
	"admin_only":      "Admins only.",
	"add_ok":          "✅ User <code>%s</code> authorized (Telegram ID %d).",
	"add_fail":        "Could not authorize.",
	"upload_soon":     "Upload via the bot is not available yet — use the web UI.",
	"unknown_cmd":     "Unknown command. /help for help.",
}

const helpRU = `📚 <b>Полка</b> — телеграм-бот библиотеки

Искать можно так:
• просто текст — полный поиск
• <code>/search …</code>, <code>/title …</code>, <code>/author …</code>, <code>/series …</code>
• <code>/id 12345</code> — книга по ID
• <code>/random</code> — 10 случайных
• <code>/stats</code> — размер библиотеки
• <code>/language ru|en</code>

Поля запроса:
<code>title:</code> <code>author:</code> <code>series:</code> <code>tags:</code> <code>languages:</code> <code>formats:</code>
Точное совпадение — <code>:=</code>, сочетания — <code>and</code>.

Примеры:
<code>Гарри Поттер</code>
<code>title:=Война и мир</code>
<code>series:=Дозоры and author:Лукьяненко</code>

Админ: <code>/add &lt;telegram_id&gt;</code> — выдать доступ.`

const helpEN = `📚 <b>Polka</b> — library Telegram bot

Search with:
• plain text — full-text search
• <code>/search …</code>, <code>/title …</code>, <code>/author …</code>, <code>/series …</code>
• <code>/id 12345</code> — book by ID
• <code>/random</code> — 10 random books
• <code>/stats</code> — library size
• <code>/language ru|en</code>

Query fields:
<code>title:</code> <code>author:</code> <code>series:</code> <code>tags:</code> <code>languages:</code> <code>formats:</code>
Exact match — <code>:=</code>, combine with <code>and</code>.

Examples:
<code>Harry Potter</code>
<code>series:=Watch and author:Lukyanenko</code>

Admin: <code>/add &lt;telegram_id&gt;</code> — grant access.`

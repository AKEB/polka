package tgbot

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/vestigiumincaligne/polka/internal/auth"
	"github.com/vestigiumincaligne/polka/internal/library"
	"github.com/vestigiumincaligne/polka/internal/store"
)

const (
	pageSize     = 5
	sessionTTL   = 30 * time.Minute
	maxCaption   = 1024
	maxMessage   = 4000
)

// Bot is a long-polling Telegram client for Polka.
type Bot struct {
	api   *tgbotapi.BotAPI
	st    *store.Store
	users *auth.Service
	lib   *library.Library
	log   *slog.Logger

	mu       sync.Mutex
	sessions map[string]*searchSession
	langs    map[int64]string // telegram user id → "ru"|"en"
}

type searchSession struct {
	filters []store.SearchFilter
	ids     []int64
	total   int
	expires time.Time
}

// New creates a bot. Call Run in a goroutine.
func New(token string, st *store.Store, users *auth.Service, lib *library.Library, log *slog.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	api.Debug = false
	return &Bot{
		api:      api,
		st:       st,
		users:    users,
		lib:      lib,
		log:      log,
		sessions: make(map[string]*searchSession),
		langs:    make(map[int64]string),
	}, nil
}

// Run polls Telegram until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	b.log.Info("telegram bot starting", "username", b.api.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := b.api.GetUpdatesChan(u)
	go b.gcSessions(ctx)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return ctx.Err()
		case upd, ok := <-updates:
			if !ok {
				return nil
			}
			b.handleUpdate(ctx, upd)
		}
	}
}

func (b *Bot) gcSessions(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			b.mu.Lock()
			for id, s := range b.sessions {
				if now.After(s.expires) {
					delete(b.sessions, id)
				}
			}
			b.mu.Unlock()
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, upd tgbotapi.Update) {
	defer func() {
		if rec := recover(); rec != nil {
			b.log.Error("telegram handler panic", "err", rec)
		}
	}()
	if upd.CallbackQuery != nil {
		b.onCallback(ctx, upd.CallbackQuery)
		return
	}
	if upd.Message == nil {
		return
	}
	msg := upd.Message
	if msg.IsCommand() {
		b.onCommand(ctx, msg)
		return
	}
	if msg.Text != "" {
		b.onSearch(ctx, msg, msg.Text)
	}
}

func (b *Bot) onCommand(ctx context.Context, msg *tgbotapi.Message) {
	cmd := msg.Command()
	args := strings.TrimSpace(msg.CommandArguments())
	switch cmd {
	case "start", "help":
		b.replyHTML(msg.Chat.ID, helpText(b.lang(msg.From.ID)))
	case "search":
		if args == "" {
			b.reply(msg.Chat.ID, b.tr(msg.From.ID, "usage_search"))
			return
		}
		b.onSearch(ctx, msg, args)
	case "title":
		b.onSearch(ctx, msg, "title:"+args)
	case "author":
		b.onSearch(ctx, msg, "author:"+args)
	case "series":
		b.onSearch(ctx, msg, "series:"+args)
	case "id":
		b.onID(ctx, msg, args)
	case "random":
		b.onRandom(ctx, msg)
	case "stats":
		b.onStats(ctx, msg)
	case "language", "lang":
		b.onLanguage(msg, args)
	case "add":
		b.onAdd(ctx, msg, args)
	case "upload":
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "upload_soon"))
	default:
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "unknown_cmd"))
	}
}

func (b *Bot) requireUser(msg *tgbotapi.Message) *auth.User {
	if msg.From == nil {
		return nil
	}
	u, err := b.users.GetByTelegramID(context.Background(), msg.From.ID)
	if err != nil {
		b.replyHTML(msg.Chat.ID, b.tr(msg.From.ID, "not_authorized", msg.From.ID, msg.From.ID))
		return nil
	}
	return u
}

func (b *Bot) onSearch(ctx context.Context, msg *tgbotapi.Message, raw string) {
	if b.requireUser(msg) == nil {
		return
	}
	filters := ParseQuery(raw)
	if len(filters) == 0 {
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "empty_query"))
		return
	}
	books, total, err := b.st.SearchFiltered(ctx, filters, 500, 0)
	if err != nil {
		b.log.Warn("tg search", "error", err)
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "search_fail"))
		return
	}
	if total == 0 {
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "no_results"))
		return
	}
	ids := make([]int64, len(books))
	for i, bk := range books {
		ids[i] = bk.ID
	}
	sid := b.saveSession(filters, ids, total)
	b.sendResultsPage(msg.Chat.ID, msg.From.ID, sid, 0, books, total)
}

func (b *Bot) onID(ctx context.Context, msg *tgbotapi.Message, args string) {
	if b.requireUser(msg) == nil {
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
	if err != nil || id <= 0 {
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "usage_id"))
		return
	}
	b.sendBookCard(ctx, msg.Chat.ID, msg.From.ID, id, "")
}

func (b *Bot) onRandom(ctx context.Context, msg *tgbotapi.Message) {
	if b.requireUser(msg) == nil {
		return
	}
	books, err := b.st.RandomBooks(ctx, 10)
	if err != nil || len(books) == 0 {
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "no_results"))
		return
	}
	ids := make([]int64, len(books))
	for i, bk := range books {
		ids[i] = bk.ID
	}
	sid := b.saveSession(nil, ids, len(books))
	b.sendResultsPage(msg.Chat.ID, msg.From.ID, sid, 0, books, len(books))
}

func (b *Bot) onStats(ctx context.Context, msg *tgbotapi.Message) {
	if b.requireUser(msg) == nil {
		return
	}
	n, err := b.st.BookCount(ctx)
	if err != nil {
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "search_fail"))
		return
	}
	b.replyHTML(msg.Chat.ID, b.tr(msg.From.ID, "stats", n))
}

func (b *Bot) onLanguage(msg *tgbotapi.Message, args string) {
	args = strings.ToLower(strings.TrimSpace(args))
	switch args {
	case "ru", "en":
		b.mu.Lock()
		b.langs[msg.From.ID] = args
		b.mu.Unlock()
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "lang_set"))
	default:
		b.reply(msg.Chat.ID, "Usage: /language ru | /language en")
	}
}

func (b *Bot) onAdd(ctx context.Context, msg *tgbotapi.Message, args string) {
	admin, err := b.users.GetByTelegramID(ctx, msg.From.ID)
	if err != nil || !admin.IsAdmin() {
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "admin_only"))
		return
	}
	tgID, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
	if err != nil || tgID <= 0 {
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "usage_add"))
		return
	}
	u, err := b.users.AuthorizeTelegram(ctx, tgID, "")
	if err != nil {
		b.log.Warn("tg authorize", "error", err)
		b.reply(msg.Chat.ID, b.tr(msg.From.ID, "add_fail"))
		return
	}
	b.replyHTML(msg.Chat.ID, b.tr(msg.From.ID, "add_ok", u.Login, tgID))
}

func (b *Bot) onCallback(ctx context.Context, cq *tgbotapi.CallbackQuery) {
	data := cq.Data
	chatID := cq.Message.Chat.ID
	fromID := cq.From.ID
	_, _ = b.api.Request(tgbotapi.NewCallback(cq.ID, ""))

	if u, err := b.users.GetByTelegramID(ctx, fromID); err != nil || u == nil {
		b.replyHTML(chatID, b.tr(fromID, "not_authorized", fromID, fromID))
		return
	}

	switch {
	case strings.HasPrefix(data, "p:"): // p:<sid>:<page>
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			return
		}
		page, _ := strconv.Atoi(parts[2])
		b.sendSessionPage(ctx, chatID, fromID, parts[1], page, cq.Message.MessageID)
	case strings.HasPrefix(data, "b:"): // b:<bookID>:<sid>:<page>
		parts := strings.Split(data, ":")
		if len(parts) < 2 {
			return
		}
		id, _ := strconv.ParseInt(parts[1], 10, 64)
		back := ""
		if len(parts) >= 4 {
			back = parts[2] + ":" + parts[3]
		}
		b.sendBookCard(ctx, chatID, fromID, id, back)
	case strings.HasPrefix(data, "d:"): // d:<bookID>:<fmt>
		parts := strings.Split(data, ":")
		if len(parts) != 3 {
			return
		}
		id, _ := strconv.ParseInt(parts[1], 10, 64)
		b.sendDownload(ctx, chatID, fromID, id, parts[2])
	case data == "cancel":
		if cq.Message != nil {
			_, _ = b.api.Request(tgbotapi.NewDeleteMessage(chatID, cq.Message.MessageID))
		}
	}
}

func (b *Bot) saveSession(filters []store.SearchFilter, ids []int64, total int) string {
	sid := fmt.Sprintf("%d", time.Now().UnixNano())
	b.mu.Lock()
	b.sessions[sid] = &searchSession{
		filters: filters,
		ids:     ids,
		total:   total,
		expires: time.Now().Add(sessionTTL),
	}
	b.mu.Unlock()
	return sid
}

func (b *Bot) getSession(sid string) *searchSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sessions[sid]
	if s == nil || time.Now().After(s.expires) {
		delete(b.sessions, sid)
		return nil
	}
	s.expires = time.Now().Add(sessionTTL)
	return s
}

func (b *Bot) sendSessionPage(ctx context.Context, chatID, fromID int64, sid string, page, editMsgID int) {
	sess := b.getSession(sid)
	if sess == nil {
		b.reply(chatID, b.tr(fromID, "session_expired"))
		return
	}
	start := page * pageSize
	if start >= len(sess.ids) {
		return
	}
	end := start + pageSize
	if end > len(sess.ids) {
		end = len(sess.ids)
	}
	books, err := b.st.BooksByIDs(ctx, sess.ids[start:end])
	if err != nil {
		b.reply(chatID, b.tr(fromID, "search_fail"))
		return
	}
	// Preserve order of ids slice.
	byID := map[int64]store.Book{}
	for _, bk := range books {
		byID[bk.ID] = bk
	}
	ordered := make([]store.Book, 0, end-start)
	for _, id := range sess.ids[start:end] {
		if bk, ok := byID[id]; ok {
			ordered = append(ordered, bk)
		}
	}
	text, keyboard := b.formatResults(fromID, sid, page, ordered, sess.total)
	if editMsgID > 0 {
		edit := tgbotapi.NewEditMessageText(chatID, editMsgID, text)
		edit.ParseMode = "HTML"
		edit.ReplyMarkup = &keyboard
		if _, err := b.api.Send(edit); err != nil {
			b.log.Warn("tg edit", "error", err)
		}
		return
	}
	b.sendResultsPage(chatID, fromID, sid, page, ordered, sess.total)
}

func (b *Bot) sendResultsPage(chatID, fromID int64, sid string, page int, pageBooks []store.Book, total int) {
	text, keyboard := b.formatResults(fromID, sid, page, pageBooks, total)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	if _, err := b.api.Send(msg); err != nil {
		b.log.Warn("tg send results", "error", err)
	}
}

func (b *Bot) formatResults(fromID int64, sid string, page int, books []store.Book, total int) (string, tgbotapi.InlineKeyboardMarkup) {
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(b.tr(fromID, "found_header"), total, page+1, pages))
	sb.WriteByte('\n')
	start := page * pageSize
	for i, bk := range books {
		n := start + i + 1
		sb.WriteString(fmt.Sprintf("\n<b>%d.</b> 📚 %s <i>(%s)</i>\n", n, html.EscapeString(bk.Title), html.EscapeString(langLabel(bk.Lang))))
		if bk.AuthorNames != "" {
			sb.WriteString("✍️ " + html.EscapeString(bk.AuthorNames) + "\n")
		}
		sb.WriteString(fmt.Sprintf("ID: <code>%d</code> · %s\n", bk.ID, formatSize(bk.Size)))
		if bk.SeriesTitle != "" {
			seq := ""
			if bk.SeqNumber > 0 {
				seq = fmt.Sprintf(" [%d]", bk.SeqNumber)
			}
			sb.WriteString("📖 " + html.EscapeString(bk.SeriesTitle) + seq + "\n")
		}
	}
	text := trimLen(sb.String(), maxMessage)

	var rows [][]tgbotapi.InlineKeyboardButton
	var pick []tgbotapi.InlineKeyboardButton
	for i, bk := range books {
		pick = append(pick, tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("📚 %d", start+i+1),
			fmt.Sprintf("b:%d:%s:%d", bk.ID, sid, page),
		))
	}
	rows = append(rows, pick)
	var nav []tgbotapi.InlineKeyboardButton
	if page > 0 {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("⬅️ "+b.tr(fromID, "prev"), fmt.Sprintf("p:%s:%d", sid, page-1)))
	}
	if (page+1)*pageSize < total {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData(b.tr(fromID, "next")+" ➡️", fmt.Sprintf("p:%s:%d", sid, page+1)))
	}
	if len(nav) > 0 {
		rows = append(rows, nav)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❌ "+b.tr(fromID, "cancel"), "cancel"),
	))
	return text, tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (b *Bot) sendBookCard(ctx context.Context, chatID, fromID, bookID int64, back string) {
	d, err := b.st.BookDetails(ctx, bookID)
	if err != nil {
		b.reply(chatID, b.tr(fromID, "book_missing"))
		return
	}
	caption := b.formatBookCard(fromID, d)
	keyboard := b.bookKeyboard(fromID, d, back)

	var cover []byte
	if b.lib != nil {
		cover, _, _ = b.lib.Cover(d.ID, d.Folder, d.File, d.Ext)
	}
	if len(cover) > 0 {
		photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{Name: "cover.jpg", Bytes: cover})
		photo.Caption = trimLen(caption, maxCaption)
		photo.ParseMode = "HTML"
		photo.ReplyMarkup = keyboard
		if _, err := b.api.Send(photo); err != nil {
			b.log.Warn("tg photo", "error", err)
			b.sendBookText(chatID, caption, keyboard)
		}
		return
	}
	b.sendBookText(chatID, caption, keyboard)
}

func (b *Bot) sendBookText(chatID int64, caption string, keyboard tgbotapi.InlineKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, trimLen(caption, maxMessage))
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = keyboard
	if _, err := b.api.Send(msg); err != nil {
		b.log.Warn("tg book card", "error", err)
	}
}

func (b *Bot) formatBookCard(fromID int64, d *store.BookDetails) string {
	var sb strings.Builder
	sb.WriteString("<b>" + html.EscapeString(d.Title) + "</b>\n")
	if d.AuthorNames != "" {
		sb.WriteString("✍️ " + html.EscapeString(d.AuthorNames) + "\n")
	}
	if d.SeriesTitle != "" {
		seq := ""
		if d.SeqNumber > 0 {
			seq = fmt.Sprintf(" [%d]", d.SeqNumber)
		}
		sb.WriteString(b.tr(fromID, "series_label") + " " + html.EscapeString(d.SeriesTitle) + seq + "\n")
	}
	sb.WriteString(fmt.Sprintf("ID: <code>%d</code>\n", d.ID))
	if len(d.Genres) > 0 {
		sb.WriteString(b.tr(fromID, "genre_label") + " " + html.EscapeString(strings.Join(d.Genres, ", ")) + "\n")
	}
	sb.WriteString(b.tr(fromID, "lang_label") + " " + html.EscapeString(langLabel(d.Lang)) + "\n")
	sb.WriteString(formatSize(d.Size) + " · ." + d.Ext + "\n")

	if b.lib != nil {
		if meta, err := b.lib.Meta(d.Folder, d.File, d.Ext); err == nil && meta != nil && meta.AnnotationHTML != "" {
			plain := stripTags(meta.AnnotationHTML)
			plain = trimLen(plain, 600)
			if plain != "" {
				sb.WriteString("\n<i>" + html.EscapeString(plain) + "</i>\n")
			}
		}
	}
	sb.WriteString("\n" + b.tr(fromID, "pick_format"))
	return sb.String()
}

func (b *Bot) bookKeyboard(fromID int64, d *store.BookDetails, back string) tgbotapi.InlineKeyboardMarkup {
	ext := strings.ToLower(d.Ext)
	type fmtBtn struct {
		label, fmt string
		ready      bool
	}
	var btns []fmtBtn
	add := func(label, fmtName string, ready bool) {
		btns = append(btns, fmtBtn{label, fmtName, ready})
	}
	// Native format
	add(strings.ToUpper(ext), "native", true)
	if ext == "fb2" {
		add("FB2.ZIP", "zip", true)
		add("FB2 "+b.tr(fromID, "compact"), "compact", true)
	}
	if library.CanConvert(ext, "epub") && ext != "epub" {
		add("EPUB", "epub", false)
	}
	if library.CanConvert(ext, "fb2") && ext != "fb2" {
		add("FB2", "fb2", false)
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	var row []tgbotapi.InlineKeyboardButton
	for _, btn := range btns {
		mark := "✅"
		if !btn.ready {
			mark = "🔄"
		}
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(
			mark+" "+btn.label,
			fmt.Sprintf("d:%d:%s", d.ID, btn.fmt),
		))
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	var nav []tgbotapi.InlineKeyboardButton
	if back != "" {
		parts := strings.SplitN(back, ":", 2)
		if len(parts) == 2 {
			nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("⬅️ "+b.tr(fromID, "back"), "p:"+parts[0]+":"+parts[1]))
		}
	}
	nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("❌ "+b.tr(fromID, "cancel"), "cancel"))
	rows = append(rows, nav)
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (b *Bot) sendDownload(ctx context.Context, chatID, fromID, bookID int64, fmtName string) {
	if b.lib == nil {
		b.reply(chatID, b.tr(fromID, "no_library"))
		return
	}
	d, err := b.st.BookDetails(ctx, bookID)
	if err != nil {
		b.reply(chatID, b.tr(fromID, "book_missing"))
		return
	}
	wait, _ := b.api.Send(tgbotapi.NewMessage(chatID, b.tr(fromID, "preparing")))

	var data []byte
	var filename string

	switch fmtName {
	case "native":
		rc, _, err := b.lib.Open(d.Folder, d.File, d.Ext)
		if err != nil {
			b.reply(chatID, b.tr(fromID, "download_fail"))
			return
		}
		data, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			b.reply(chatID, b.tr(fromID, "download_fail"))
			return
		}
		filename = sanitizeName(d.Title) + "." + d.Ext
	case "zip":
		rc, _, err := b.lib.Open(d.Folder, d.File, d.Ext)
		if err != nil {
			b.reply(chatID, b.tr(fromID, "download_fail"))
			return
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			b.reply(chatID, b.tr(fromID, "download_fail"))
			return
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create(d.File + "." + d.Ext)
		_, _ = w.Write(raw)
		_ = zw.Close()
		data = buf.Bytes()
		filename = sanitizeName(d.Title) + "." + d.Ext + ".zip"
	case "compact":
		rc, _, err := b.lib.Open(d.Folder, d.File, d.Ext)
		if err != nil {
			b.reply(chatID, b.tr(fromID, "download_fail"))
			return
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			b.reply(chatID, b.tr(fromID, "download_fail"))
			return
		}
		data = library.StripBinaries(raw)
		filename = sanitizeName(d.Title) + ".compact.fb2"
	case "epub", "fb2":
		info := library.ConvertInfo{Title: d.Title, Lang: d.Lang}
		for _, a := range d.Authors {
			info.Authors = append(info.Authors, library.PersonName{First: a.First, Middle: a.Middle, Last: a.Last})
		}
		if len(d.Series) > 0 {
			info.Series = d.Series[0].Title
			info.SeqNum = d.Series[0].SeqNumber
		}
		data, err = b.lib.ConvertBook(d.Folder, d.File, d.Ext, fmtName, info)
		if err != nil {
			b.log.Warn("tg convert", "error", err)
			b.reply(chatID, b.tr(fromID, "download_fail"))
			return
		}
		filename = sanitizeName(d.Title) + "." + fmtName
	default:
		b.reply(chatID, b.tr(fromID, "download_fail"))
		return
	}

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{Name: filename, Bytes: data})
	doc.Caption = d.Title
	if _, err := b.api.Send(doc); err != nil {
		b.log.Warn("tg document", "error", err, "size", len(data))
		b.reply(chatID, b.tr(fromID, "download_fail"))
	}
	if wait.MessageID != 0 {
		_, _ = b.api.Request(tgbotapi.NewDeleteMessage(chatID, wait.MessageID))
	}
}

func (b *Bot) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		b.log.Warn("tg reply", "error", err)
	}
}

func (b *Bot) replyHTML(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	if _, err := b.api.Send(msg); err != nil {
		b.log.Warn("tg reply", "error", err)
	}
}

func (b *Bot) lang(tgID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if l := b.langs[tgID]; l != "" {
		return l
	}
	return "ru"
}

func formatSize(n int64) string {
	if n > 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
	if n > 1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%dB", n)
}

func langLabel(code string) string {
	switch strings.ToLower(code) {
	case "ru":
		return "rus"
	case "en":
		return "eng"
	case "":
		return "?"
	default:
		return code
	}
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "book"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 32 || strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if utf8.RuneCountInString(out) > 80 {
		runes := []rune(out)
		out = string(runes[:80])
	}
	return out
}

func trimLen(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n-1]) + "…"
}

func stripTags(htmlStr string) string {
	var b strings.Builder
	inTag := false
	for _, r := range htmlStr {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteByte(' ')
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

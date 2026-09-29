import { t } from "../i18n";
import { useMemo, useState } from "react";
import { updateBook } from "../api/manage";
import "./BookEditForm.css";

const emptyAuthor = () => ({ last: "", first: "", middle: "" });

const authorsFrom = (data) => {
  const list = (data?.authors ?? []).map((a) => ({
    last: a.LastName || "",
    first: a.FirstName || "",
    middle: a.MiddleName || "",
  }));
  return list.length ? list : [emptyAuthor()];
};

const BookEditForm = ({ data, onSaved, onCancel }) => {
  const bf = data.bookForm;
  const [title, setTitle] = useState(bf.Title || "");
  const [authors, setAuthors] = useState(() => authorsFrom(data));
  const [series, setSeries] = useState(data.series?.[0]?.SeriesTitle ?? "");
  const [seriesNum, setSeriesNum] = useState(data.series?.[0]?.SeqNumber || "");
  const [year, setYear] = useState(bf.Year || "");
  const [lang, setLang] = useState(bf.Lang || "");
  const [isbn, setIsbn] = useState(bf.ISBN || data.isbn || "");
  const [genres, setGenres] = useState(() => (data.genresList ?? []).map((g) => g.code));
  const [genreQuery, setGenreQuery] = useState("");
  const [busy, setBusy] = useState(false);

  const catalog = data.genreCatalog ?? [];
  const languages = data.languages ?? [];
  const filteredGenres = useMemo(() => {
    const q = genreQuery.trim().toLowerCase();
    if (!q) return catalog;
    return catalog.filter(
      (g) => g.name.toLowerCase().includes(q) || g.code.toLowerCase().includes(q)
    );
  }, [catalog, genreQuery]);

  const setAuthor = (i, field, value) => {
    setAuthors((prev) => prev.map((a, idx) => (idx === i ? { ...a, [field]: value } : a)));
  };

  const save = () => {
    if (!title.trim() || busy) return;
    setBusy(true);
    updateBook(bf.BookID, {
      title: title.trim(),
      authors,
      series: series.trim(),
      seriesNum: Number(seriesNum) || 0,
      year: Number(year) || 0,
      lang,
      genres,
      isbn: isbn.trim(),
    })
      .then(onSaved)
      .catch(() => alert(t("book.edit.fail")))
      .finally(() => setBusy(false));
  };

  const toggleGenre = (code) => {
    setGenres((prev) => (prev.includes(code) ? prev.filter((c) => c !== code) : [...prev, code]));
  };

  return (
    <form
      className="book-edit"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <h2 className="book-edit__title">{t("book.edit")}</h2>

      <label className="book-edit__field">
        <span>{t("book.edit.title")}</span>
        <input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </label>

      <fieldset className="book-edit__authors">
        <legend>{t("book.edit.authors")}</legend>
        {authors.map((a, i) => (
          <div className="book-edit__author" key={i}>
            <input
              placeholder={t("book.edit.author.last")}
              value={a.last}
              onChange={(e) => setAuthor(i, "last", e.target.value)}
            />
            <input
              placeholder={t("book.edit.author.first")}
              value={a.first}
              onChange={(e) => setAuthor(i, "first", e.target.value)}
            />
            <input
              placeholder={t("book.edit.author.middle")}
              value={a.middle}
              onChange={(e) => setAuthor(i, "middle", e.target.value)}
            />
            <button
              type="button"
              className="btn btn-ghost book-edit__remove"
              onClick={() => setAuthors((prev) => (prev.length > 1 ? prev.filter((_, idx) => idx !== i) : [emptyAuthor()]))}
              aria-label={t("book.edit.author.remove")}
            >
              ×
            </button>
          </div>
        ))}
        <button type="button" className="btn btn-link" onClick={() => setAuthors((prev) => [...prev, emptyAuthor()])}>
          {t("book.edit.author.add")}
        </button>
      </fieldset>

      <div className="book-edit__row">
        <label className="book-edit__field book-edit__field--grow">
          <span>{t("book.edit.series")}</span>
          <input value={series} onChange={(e) => setSeries(e.target.value)} />
        </label>
        <label className="book-edit__field book-edit__num">
          <span>{t("book.edit.seriesNum")}</span>
          <input type="number" min="0" value={seriesNum} onChange={(e) => setSeriesNum(e.target.value)} />
        </label>
      </div>

      <div className="book-edit__row">
        <label className="book-edit__field book-edit__num">
          <span>{t("book.edit.year")}</span>
          <input type="number" min="0" max="2100" value={year} onChange={(e) => setYear(e.target.value)} />
        </label>
        <label className="book-edit__field">
          <span>{t("book.edit.lang")}</span>
          <input
            list="book-edit-langs"
            value={lang}
            onChange={(e) => setLang(e.target.value)}
            placeholder="ru"
          />
          <datalist id="book-edit-langs">
            {languages.map((l) => (
              <option key={l.code} value={l.code === "-" ? "" : l.code}>
                {l.name}
              </option>
            ))}
          </datalist>
        </label>
        <label className="book-edit__field book-edit__field--grow">
          <span>{t("book.edit.isbn")}</span>
          <input value={isbn} onChange={(e) => setIsbn(e.target.value)} />
        </label>
      </div>

      {catalog.length > 0 && (
        <fieldset className="book-edit__genres">
          <legend>{t("book.edit.genres")}</legend>
          <input
            className="book-edit__genre-search"
            value={genreQuery}
            onChange={(e) => setGenreQuery(e.target.value)}
            placeholder={t("book.edit.genres.search")}
          />
          <div className="book-edit__genre-list">
            {filteredGenres.map((g) => (
              <label key={g.code} className="book-edit__genre">
                <input
                  type="checkbox"
                  checked={genres.includes(g.code)}
                  onChange={() => toggleGenre(g.code)}
                />
                {g.name}
              </label>
            ))}
          </div>
        </fieldset>
      )}

      <div className="book-edit__actions">
        <button type="submit" className="btn btn-primary" disabled={busy || !title.trim()}>
          {busy ? t("loading") : t("book.edit.save")}
        </button>
        <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={busy}>
          {t("book.edit.cancel")}
        </button>
      </div>
    </form>
  );
};

export default BookEditForm;

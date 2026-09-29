import { t } from "../i18n";
import { useState } from "react";
import { Link } from "react-router-dom";
import api from "../api/api";
import RatingStars from "./RatingStars";
import "./BookCard.css";

const BookCard = ({ book, size = "md", onForget }) => {
  const [coverFailed, setCoverFailed] = useState(false);

  if (!book) return null;

  const { BookID, Title, AuthorsNames, SeriesTitle, SeqNumber, Year, LibRate, ReadingProgress, Finished } = book;

  const initials = (Title || "").trim().slice(0, 2).toUpperCase();
  const seriesLabel =
    SeriesTitle && SeqNumber
      ? `${SeriesTitle} · ${SeqNumber}`
      : SeriesTitle || (Year ? `${Year}` : "");
  const isFinished = Boolean(Finished) || (typeof ReadingProgress === "number" && ReadingProgress >= 0.98);
  const showProgress = typeof ReadingProgress === "number" && ReadingProgress < 0.98;

  return (
    <article className={`book-card book-card--${size}${isFinished ? " book-card--finished" : ""}`}>
      <div className="book-card__cover-wrap">
        <Link to={`/book/${BookID}`} className="book-card__cover-link" aria-label={Title}>
          <div className="book-card__cover">
            {coverFailed ? (
              <div className="book-card__cover-fallback">
                <span>{initials || "?"}</span>
              </div>
            ) : (
              <img
                src={api.coverUrl(BookID)}
                alt=""
                loading="lazy"
                onError={() => setCoverFailed(true)}
              />
            )}
            {isFinished && (
              <div className="book-card__finished" title={t("book.finished.on")}>
                ✓
              </div>
            )}
            {LibRate > 0 && (
              <div className="book-card__rating-badge">
                <RatingStars value={LibRate} size="sm" showNumeric={false} />
              </div>
            )}
            {showProgress && (
              <div className="book-card__progress" title={`${Math.round(ReadingProgress * 100)}%`}>
                <div
                  className="book-card__progress-fill"
                  style={{ width: `${Math.max(2, Math.round(ReadingProgress * 100))}%` }}
                />
              </div>
            )}
          </div>
        </Link>
        {onForget && (
          <button
            type="button"
            className="book-card__forget"
            title={t("shelf.forget")}
            aria-label={t("shelf.forget")}
            onClick={() => onForget(book)}
          >
            ×
          </button>
        )}
      </div>

      <div className="book-card__meta">
        <Link to={`/book/${BookID}`} className="book-card__title" title={Title}>
          {Title}
        </Link>
        {AuthorsNames && (
          <div className="book-card__author" title={AuthorsNames}>
            {AuthorsNames}
          </div>
        )}
        {seriesLabel && <div className="book-card__series">{seriesLabel}</div>}
      </div>
    </article>
  );
};

export default BookCard;

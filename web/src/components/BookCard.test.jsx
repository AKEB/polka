import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { t } from "../i18n";
import BookCard from "./BookCard";

const book = {
  BookID: 42,
  Title: "Война и мир",
  AuthorsNames: "Толстой Лев",
  SeriesTitle: "Классика",
  SeqNumber: 1,
  Year: 1869,
  LibRate: 4.5,
};

const renderCard = (b) => render(<MemoryRouter><BookCard book={b} /></MemoryRouter>);

describe("BookCard", () => {
  it("renders nothing without a book", () => {
    const { container } = render(<MemoryRouter><BookCard book={null} /></MemoryRouter>);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows title, author, series and links to the book page", () => {
    renderCard(book);
    expect(screen.getByText("Война и мир")).toHaveAttribute("href", "/book/42");
    expect(screen.getByText("Толстой Лев")).toBeInTheDocument();
    expect(screen.getByText("Классика · 1")).toBeInTheDocument();
    expect(document.querySelector(".book-card__cover img")).toHaveAttribute("src", "/Images/covers/42");
    expect(document.querySelector(".book-card__rating-badge")).not.toBeNull();
    expect(document.querySelector(".book-card__progress")).toBeNull();
  });

  it("falls back to the year when there is no series, hides an empty rating", () => {
    renderCard({ ...book, SeriesTitle: "", SeqNumber: 0, LibRate: 0 });
    expect(screen.getByText("1869")).toBeInTheDocument();
    expect(document.querySelector(".book-card__rating-badge")).toBeNull();
  });

  it("replaces a broken cover with initials", () => {
    renderCard(book);
    fireEvent.error(document.querySelector(".book-card__cover img"));
    expect(document.querySelector(".book-card__cover img")).toBeNull();
    expect(screen.getByText("ВО")).toBeInTheDocument();
  });

  it("can forget a book from Reading now without following the cover link", () => {
    const onForget = vi.fn();
    render(<MemoryRouter><BookCard book={{ ...book, ReadingProgress: 0.2 }} onForget={onForget} /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: t("shelf.forget") }));
    expect(onForget).toHaveBeenCalledOnce();
  });

  it("draws the reading progress bar", () => {
    renderCard({ ...book, ReadingProgress: 0.4 });
    const bar = document.querySelector(".book-card__progress");
    expect(bar).toHaveAttribute("title", "40%");
    expect(document.querySelector(".book-card__progress-fill").style.width).toBe("40%");
    // Tiny progress is still visible.
    renderCard({ ...book, BookID: 43, ReadingProgress: 0.001 });
    const fills = document.querySelectorAll(".book-card__progress-fill");
    expect(fills[fills.length - 1].style.width).toBe("2%");
  });

  it("marks finished books with a check badge instead of a full progress bar", () => {
    renderCard({ ...book, Finished: true, ReadingProgress: 1 });
    expect(document.querySelector(".book-card__finished")).toHaveAttribute("title", t("book.finished.on"));
    expect(document.querySelector(".book-card--finished")).not.toBeNull();
    expect(document.querySelector(".book-card__progress")).toBeNull();
  });
});

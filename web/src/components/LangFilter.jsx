import { t } from "../i18n";
import "./LangFilter.css";

const LangFilter = ({ languages, value, onChange }) => {
  if (!languages?.length) return null;
  if (languages.length < 2 && !value) return null;
  return (
    <div className="lang-filter" role="group" aria-label={t("filter.lang")}>
      <button
        type="button"
        className={`lang-filter__chip ${!value ? "is-active" : ""}`}
        onClick={() => onChange("")}
      >
        {t("filter.lang.all")}
      </button>
      {languages.map((l) => (
        <button
          type="button"
          key={l.code}
          className={`lang-filter__chip ${value === l.code ? "is-active" : ""}`}
          onClick={() => onChange(l.code)}
        >
          {l.name}
          <span className="lang-filter__count">{l.books}</span>
        </button>
      ))}
    </div>
  );
};

export default LangFilter;

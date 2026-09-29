// Package langs maps catalog language codes (inpx LANG, FB2 <lang>)
// to localized display names.
package langs

import "strings"

// Unknown is the filter code for books with an empty lang column.
const Unknown = "-"

// Name returns the language name in ui ("ru" or "en"). Unknown codes
// are shown uppercased (EN, DE). Empty / "-" is "Unknown" / "Не указан".
func Name(code, ui string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" || code == Unknown || code == "und" {
		if ui == "en" {
			return "Unknown"
		}
		return "Не указан"
	}
	table := namesRU
	if ui == "en" {
		table = namesEN
	}
	if n, ok := table[code]; ok {
		return n
	}
	return strings.ToUpper(code)
}

var namesRU = map[string]string{
	"ru": "Русский", "en": "Английский", "uk": "Украинский", "be": "Белорусский",
	"de": "Немецкий", "fr": "Французский", "es": "Испанский", "it": "Итальянский",
	"pl": "Польский", "cs": "Чешский", "sk": "Словацкий", "bg": "Болгарский",
	"sr": "Сербский", "hr": "Хорватский", "sl": "Словенский", "mk": "Македонский",
	"pt": "Португальский", "nl": "Нидерландский", "sv": "Шведский", "no": "Норвежский",
	"da": "Датский", "fi": "Финский", "et": "Эстонский", "lv": "Латышский",
	"lt": "Литовский", "hu": "Венгерский", "ro": "Румынский", "el": "Греческий",
	"tr": "Турецкий", "he": "Иврит", "ar": "Арабский", "fa": "Персидский",
	"zh": "Китайский", "ja": "Японский", "ko": "Корейский", "vi": "Вьетнамский",
	"th": "Тайский", "hi": "Хинди", "la": "Латынь", "eo": "Эсперанто",
	"ka": "Грузинский", "hy": "Армянский", "kk": "Казахский", "az": "Азербайджанский",
	"uz": "Узбекский", "tt": "Татарский", "ba": "Башкирский", "cv": "Чувашский",
	"ky": "Киргизский", "tg": "Таджикский", "tk": "Туркменский", "mn": "Монгольский",
}

var namesEN = map[string]string{
	"ru": "Russian", "en": "English", "uk": "Ukrainian", "be": "Belarusian",
	"de": "German", "fr": "French", "es": "Spanish", "it": "Italian",
	"pl": "Polish", "cs": "Czech", "sk": "Slovak", "bg": "Bulgarian",
	"sr": "Serbian", "hr": "Croatian", "sl": "Slovenian", "mk": "Macedonian",
	"pt": "Portuguese", "nl": "Dutch", "sv": "Swedish", "no": "Norwegian",
	"da": "Danish", "fi": "Finnish", "et": "Estonian", "lv": "Latvian",
	"lt": "Lithuanian", "hu": "Hungarian", "ro": "Romanian", "el": "Greek",
	"tr": "Turkish", "he": "Hebrew", "ar": "Arabic", "fa": "Persian",
	"zh": "Chinese", "ja": "Japanese", "ko": "Korean", "vi": "Vietnamese",
	"th": "Thai", "hi": "Hindi", "la": "Latin", "eo": "Esperanto",
	"ka": "Georgian", "hy": "Armenian", "kk": "Kazakh", "az": "Azerbaijani",
	"uz": "Uzbek", "tt": "Tatar", "ba": "Bashkir", "cv": "Chuvash",
	"ky": "Kyrgyz", "tg": "Tajik", "tk": "Turkmen", "mn": "Mongolian",
}

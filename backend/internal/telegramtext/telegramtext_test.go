package telegramtext

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]Locale{
		"ru": LocaleRussian, "ru-RU": LocaleRussian, "RU": LocaleRussian,
		"en": LocaleEnglish, "en-US": LocaleEnglish, "": LocaleEnglish,
		"de": LocaleEnglish, "  ru  ": LocaleRussian,
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCatalogsAreComplete(t *testing.T) {
	for _, locale := range Supported {
		dict, ok := Catalog[locale]
		if !ok {
			t.Fatalf("no dictionary for %q", locale)
		}
		for key := range english {
			msg, ok := dict[key]
			if !ok || strings.TrimSpace(msg) == "" {
				t.Errorf("locale %q is missing a translation for %q", locale, key)
			}
		}
		for key := range dict {
			if _, ok := english[key]; !ok {
				t.Errorf("locale %q has key %q that english does not define", locale, key)
			}
		}
	}
}

func TestTFallsBackToEnglishThenKey(t *testing.T) {
	if got := T(Locale("de"), KeyHelp); got != english[KeyHelp] {
		t.Errorf("unknown locale should fall back to English, got %q", got)
	}
	if got := T(LocaleEnglish, Key("no_such_key")); got != "no_such_key" {
		t.Errorf("missing key should fall back to the key text, got %q", got)
	}
}

func TestIsSupported(t *testing.T) {
	if !IsSupported("ru-RU") || !IsSupported("en") {
		t.Error("ru-RU and en should be supported")
	}
	if IsSupported("de") || IsSupported("") {
		t.Error("de and empty should not be supported")
	}
}

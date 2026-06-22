package enrich

import "testing"

func TestDetectLanguage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "english complaint", text: "Payment form fails after submit", want: LanguageEnglish},
		{name: "english short", text: "bad login", want: LanguageEnglish},
		{name: "chinese complaint", text: "结账页面一直转圈，无法付款", want: LanguageChinese},
		{name: "chinese short", text: "退款", want: LanguageChinese},
		{name: "japanese hiragana", text: "ログインできません。支払いも失敗します", want: LanguageJapanese},
		{name: "japanese kana", text: "アプリがクラッシュします", want: LanguageJapanese},
		{name: "latin dominant mixed", text: "login failed 登录 page", want: LanguageEnglish},
		{name: "ambiguous mixed", text: "login 登录 failed 支付", want: LanguageUnknown},
		{name: "url only", text: "https://example.com/404", want: LanguageUnknown},
		{name: "symbols only", text: "!!! 500 ???", want: LanguageUnknown},
		{name: "too short", text: "x", want: LanguageUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectLanguage(tt.text); got != tt.want {
				t.Fatalf("DetectLanguage(%q)=%q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestPromptLanguageFor(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"zh":      LanguageChinese,
		"zh-CN":   LanguageChinese,
		"en":      LanguageEnglish,
		"ja":      LanguageEnglish,
		"unknown": LanguageEnglish,
		"":        LanguageEnglish,
	}
	for in, want := range tests {
		if got := promptLanguageFor(in); got != want {
			t.Fatalf("promptLanguageFor(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestNormalizeLanguage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"zh", LanguageChinese},
		{"ZH", LanguageChinese},
		{"zh-CN", LanguageChinese},
		{"zh_TW", LanguageChinese},
		{"en", LanguageEnglish},
		{"EN", LanguageEnglish},
		{"en-US", LanguageEnglish},
		{"en_GB", LanguageEnglish},
		{"ja", LanguageJapanese},
		{"ja-JP", LanguageJapanese},
		{"  zh  ", LanguageChinese},
		{"fr", LanguageUnknown},
		{"unknown", LanguageUnknown},
		{"", LanguageUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeLanguage(tt.input); got != tt.want {
				t.Errorf("normalizeLanguage(%q)=%q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDisplayLanguageForLocale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		locale string
		want   string
	}{
		{"zh", LanguageChinese},
		{"zh-CN", LanguageChinese},
		{"en", LanguageEnglish},
		{"en-US", LanguageEnglish},
		{"ja", LanguageJapanese},
		{"fr", LanguageEnglish},
		{"", LanguageEnglish},
	}
	for _, tt := range tests {
		t.Run(tt.locale, func(t *testing.T) {
			if got := displayLanguageForLocale(tt.locale); got != tt.want {
				t.Errorf("displayLanguageForLocale(%q)=%q, want %q", tt.locale, got, tt.want)
			}
		})
	}
}

func TestDisplayLocaleForTenantLocale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		locale string
		want   string
	}{
		{"zh-CN", "zh-CN"},
		{"en-US", "en-US"},
		{"  ja  ", "ja"},
		{"", LanguageEnglish},
		{"   ", LanguageEnglish},
	}
	for _, tt := range tests {
		t.Run(tt.locale, func(t *testing.T) {
			if got := displayLocaleForTenantLocale(tt.locale); got != tt.want {
				t.Errorf("displayLocaleForTenantLocale(%q)=%q, want %q", tt.locale, got, tt.want)
			}
		})
	}
}

func TestSameLanguage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		sourceLanguage string
		displayLocale  string
		want           bool
	}{
		{"same zh", "zh", "zh-CN", true},
		{"same en", "en", "en-US", true},
		{"same ja", "ja", "ja-JP", true},
		{"different", "zh", "en-US", false},
		{"source unknown", "unknown", "en-US", false},
		{"display falls back to en", "en", "fr", true},
		{"both unknown", "unknown", "unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameLanguage(tt.sourceLanguage, tt.displayLocale); got != tt.want {
				t.Errorf("sameLanguage(%q, %q)=%v, want %v", tt.sourceLanguage, tt.displayLocale, got, tt.want)
			}
		})
	}
}

func TestStripURLTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no urls", "payment failed", "payment failed"},
		{"https url", "error at https://example.com/page", "error at"},
		{"http url", "see http://test.com", "see"},
		{"www url", "visit www.example.com now", "visit now"},
		{"mixed", "try https://a.com and www.b.com later", "try and later"},
		{"empty", "", ""},
		{"only url", "https://only.com", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripURLTokens(tt.input); got != tt.want {
				t.Errorf("stripURLTokens(%q)=%q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClassifyHanLatinMix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		han   int
		latin int
		want  string
	}{
		{"han only", 10, 0, LanguageChinese},
		{"han dominant", 10, 5, LanguageChinese},
		{"han equal", 5, 5, LanguageChinese},
		{"latin dominant 3x", 2, 6, LanguageEnglish},
		{"latin equal to han", 2, 2, LanguageChinese},
		{"latin 3x but below 3", 1, 3, LanguageEnglish},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyHanLatinMix(tt.han, tt.latin); got != tt.want {
				t.Errorf("classifyHanLatinMix(%d, %d)=%q, want %q", tt.han, tt.latin, got, tt.want)
			}
		})
	}
}

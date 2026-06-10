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

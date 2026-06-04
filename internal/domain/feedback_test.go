package domain

import (
	"strings"
	"testing"
)

func TestIngestInputValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      IngestInput
		wantSub string // substring of expected error; "" means no error
	}{
		{
			name:    "happy path with all sources",
			in:      IngestInput{Content: "hello", Source: "api"},
			wantSub: "",
		},
		{
			name:    "lark-group source",
			in:      IngestInput{Content: "hello", Source: "lark-group"},
			wantSub: "",
		},
		{
			name:    "web source",
			in:      IngestInput{Content: "hello", Source: "web"},
			wantSub: "",
		},
		{
			name:    "empty content rejected",
			in:      IngestInput{Content: "", Source: "api"},
			wantSub: "content is required",
		},
		{
			name:    "content at the exact cap is accepted",
			in:      IngestInput{Content: strings.Repeat("a", MaxContentLen), Source: "api"},
			wantSub: "",
		},
		{
			name:    "content one over the cap is rejected",
			in:      IngestInput{Content: strings.Repeat("a", MaxContentLen+1), Source: "api"},
			wantSub: "too long",
		},
		{
			name:    "unknown source rejected",
			in:      IngestInput{Content: "hello", Source: "telegram"},
			wantSub: "invalid source",
		},
		{
			name:    "empty source rejected (callers must default to 'api' before Validate)",
			in:      IngestInput{Content: "hello", Source: ""},
			wantSub: "invalid source",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate()
			if tt.wantSub == "" {
				if err != nil {
					t.Fatalf("want no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("want error containing %q, got %q", tt.wantSub, err.Error())
			}
		})
	}
}

func TestSnapshotIsHighSeverity(t *testing.T) {
	tests := []struct {
		sev  string
		want bool
	}{
		{"P0", true},
		{"P1", true},
		{"P2", false},
		{"P3", false},
		{"", false},
		{"P4", false},
	}
	for _, tt := range tests {
		t.Run(tt.sev, func(t *testing.T) {
			got := Snapshot{Severity: tt.sev}.IsHighSeverity()
			if got != tt.want {
				t.Fatalf("severity %q: want %v, got %v", tt.sev, tt.want, got)
			}
		})
	}
}

func TestSeverityWeight(t *testing.T) {
	// SeverityWeight is consumed by repo.MarkDone — verify the contract
	// stays in lockstep with the priority column the SQL writes.
	want := map[string]float64{"P0": 4, "P1": 3, "P2": 2, "P3": 1}
	for sev, w := range want {
		if SeverityWeight[sev] != w {
			t.Fatalf("severity %s weight: want %v, got %v", sev, w, SeverityWeight[sev])
		}
	}
}

func TestSprint12LarkSourcesAccepted(t *testing.T) {
	// Sprint 1.2: 4 new lark-* sources must validate cleanly. If anyone
	// trims them from ValidSources, ingest-side 400s break customers
	// who configured 飞书自动化 to POST with these labels.
	for _, src := range []string{"lark-bitable", "lark-approval", "lark-helpdesk", "lark-form"} {
		in := IngestInput{Content: "x", Source: src}
		if err := in.Validate(); err != nil {
			t.Errorf("source %q must validate; got %v", src, err)
		}
	}
}

func TestSourceDisplayName(t *testing.T) {
	cases := map[string]string{
		"api":           "API 客户端",
		"lark-group":    "飞书群消息",
		"lark-bitable":  "飞书多维表格",
		"lark-approval": "飞书审批",
		"lark-helpdesk": "飞书服务台",
		"lark-form":     "飞书表单 / 文档评论",
		"email":         "邮件",
		"web":           "Web 反馈控件",
		"other":         "其他",
		// Unknown sources fall back to the raw key — never empty, never
		// panic. Dispatch envelopes rely on this for forward-compat.
		"future-source-2030": "future-source-2030",
		"":                   "",
	}
	for in, want := range cases {
		if got := SourceDisplayName(in); got != want {
			t.Errorf("SourceDisplayName(%q): want %q, got %q", in, want, got)
		}
	}
}

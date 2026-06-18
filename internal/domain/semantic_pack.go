package domain

// lint-artifacts:file-allow localized semantic-pack display data (zh/en/ja)

const (
	CustomerFeedbackPackName      = "customer_feedback"
	CustomerFeedbackSchemaVersion = "customer_feedback.v1"
)

// SemanticDomainPack is a versioned bundle of semantic Dimensions for one
// vertical. The database migration remains self-contained, but code paths,
// tests, eval, and future pack installers can depend on this typed registry.
type SemanticDomainPack struct {
	Name          string
	SchemaVersion string
	Dimensions    DimensionSet
}

func CustomerFeedbackPackV1() SemanticDomainPack {
	return SemanticDomainPack{
		Name:          CustomerFeedbackPackName,
		SchemaVersion: CustomerFeedbackSchemaVersion,
		Dimensions: DimensionSet{
			{
				Name:        "type",
				DisplayName: I18nString{"default": "Type", "zh": "类型", "en": "Type"},
				Kind:        DimSingle,
				Taxonomy: []Taxonomy{
					{Value: "bug", DisplayName: I18nString{"default": "Bug", "zh": "缺陷", "en": "Bug"}},
					{Value: "feature", DisplayName: I18nString{"default": "Feature", "zh": "特性", "en": "Feature"}},
					{Value: "question", DisplayName: I18nString{"default": "Question", "zh": "咨询", "en": "Question"}},
					{Value: "other", DisplayName: I18nString{"default": "Other", "zh": "其他", "en": "Other"}},
				},
				UrgentSet: []string{},
			},
			{
				Name:        "severity",
				DisplayName: I18nString{"default": "Severity", "zh": "严重程度", "en": "Severity"},
				Kind:        DimSingle,
				Taxonomy: []Taxonomy{
					{Value: "critical", DisplayName: I18nString{"default": "Critical", "zh": "严重", "en": "Critical"}},
					{Value: "major", DisplayName: I18nString{"default": "Major", "zh": "重要", "en": "Major"}},
					{Value: "minor", DisplayName: I18nString{"default": "Minor", "zh": "次要", "en": "Minor"}},
				},
				UrgentSet: []string{"critical"},
			},
			customerToneDimension(),
			{
				Name:        "labels",
				DisplayName: I18nString{"default": "Labels", "zh": "标签", "en": "Labels"},
				Kind:        DimMulti,
				Taxonomy:    []Taxonomy{},
				UrgentSet:   []string{},
			},
		},
	}
}

func customerToneDimension() Dimension {
	return Dimension{
		Name: "sentiment",
		DisplayName: I18nString{
			"default": "Customer tone",
			"zh":      "客户语气",
			"en":      "Customer tone",
		},
		Description: I18nString{
			"default": "The emotional tone of the user toward the product or team.",
			"zh":      "用户对产品或团队表达出的情绪状态。",
			"en":      "The emotional tone of the user toward the product or team.",
		},
		Kind: DimSingle,
		Taxonomy: []Taxonomy{
			{
				Value:       "positive",
				DisplayName: I18nString{"default": "Positive", "zh": "正向", "en": "Positive"},
				Description: I18nString{
					"default": "Praise, appreciation, delight, or successful outcome.",
					"zh":      "表扬、感谢、满意或成功完成任务。",
					"en":      "Praise, appreciation, delight, or successful outcome.",
				},
			},
			{
				Value:       "neutral",
				DisplayName: I18nString{"default": "Neutral", "zh": "中性", "en": "Neutral"},
				Description: I18nString{
					"default": "Factual report or request with little emotional charge.",
					"zh":      "事实性反馈或请求，几乎没有明显情绪。",
					"en":      "Factual report or request with little emotional charge.",
				},
			},
			{
				Value:       "negative",
				DisplayName: I18nString{"default": "Negative", "zh": "负向", "en": "Negative"},
				Description: I18nString{
					"default": "Dissatisfaction, complaint, disappointment, or criticism.",
					"zh":      "不满、抱怨、失望或批评。",
					"en":      "Dissatisfaction, complaint, disappointment, or criticism.",
				},
			},
			{
				Value:       "frustrated",
				DisplayName: I18nString{"default": "Frustrated", "zh": "挫败", "en": "Frustrated"},
				Description: I18nString{
					"default": "Repeated failure, blocked task, refund/cancel/escalation language, or patience running out.",
					"zh":      "反复失败、任务受阻、退款/取消/升级投诉，或用户耐心正在耗尽。",
					"en":      "Repeated failure, blocked task, refund/cancel/escalation language, or patience running out.",
				},
			},
		},
		Examples: []string{
			"positive: Thanks, this solved our workflow",
			"neutral: Could you add CSV export?",
			"negative: This is disappointing and confusing",
			"frustrated: I paid for this and it still fails every time",
		},
		ExtractionHint: "Use frustrated only when patience is running out: repeated failure, blocked work, refund/cancel/escalation language, or clearly heightened anger.",
		Renderer: RendererSpec{
			Kind: RendererEnumBadge,
			Values: map[string]RendererValue{
				"positive":   {Icon: RendererIconSmile, Tone: RendererToneSuccess},
				"neutral":    {Icon: RendererIconMinus, Tone: RendererToneMuted},
				"negative":   {Icon: RendererIconFrown, Tone: RendererToneWarning},
				"frustrated": {Icon: RendererIconFlame, Tone: RendererToneDanger},
			},
		},
	}
}

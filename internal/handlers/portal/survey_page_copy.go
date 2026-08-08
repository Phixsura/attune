// SPDX-License-Identifier: Apache-2.0
// lint-artifacts:file-allow canonical localized survey respondent-page copy.

package portal

import "strings"

func surveyPageCopyForLocale(locale string) surveyPageCopy {
	if isChineseSurveyLocale(locale) {
		return surveyPageCopy{
			TitleSuffix:          "Attune 调查",
			OpenUntil:            "开放至",
			HoneypotLabel:        "公司网站",
			ScoreLabel:           "评分",
			ResponseLinkHint:     "您的回复将与本邀请链接关联。",
			SubmitLabel:          "提交反馈",
			UnsubscribeLabel:     "取消订阅未来调查邮件",
			ResponseReadError:    "无法读取调查回复。",
			ChooseScore:          "请选择一个分数后再提交。",
			ChooseScaleScore:     "请选择调查量表中的分数。",
			AlreadySubmitted:     "此调查已提交。",
			DefaultThankYou:      "感谢您的反馈。",
			LowScoreFollowup:     "您的回复已标记为跟进。",
			FollowUpConsentLabel: "可以就这条反馈联系我",
			FollowUpConsentHint:  "这是可选的，仅用于本次反馈的后续沟通。",
		}
	}
	return surveyPageCopy{
		TitleSuffix:          "Attune survey",
		OpenUntil:            "Open until",
		HoneypotLabel:        "Company website",
		ScoreLabel:           "Score",
		ResponseLinkHint:     "Your response is tied to this invitation link.",
		SubmitLabel:          "Submit feedback",
		UnsubscribeLabel:     "Unsubscribe from future survey emails",
		ResponseReadError:    "The survey response could not be read.",
		ChooseScore:          "Choose a score before submitting.",
		ChooseScaleScore:     "Choose a score from the survey scale.",
		AlreadySubmitted:     "This survey has already been submitted.",
		DefaultThankYou:      "Thanks for your feedback.",
		LowScoreFollowup:     "Your response has been flagged for review.",
		FollowUpConsentLabel: "You may contact me about this feedback",
		FollowUpConsentHint:  "Optional and used only to follow up on this response.",
	}
}

func isChineseSurveyLocale(locale string) bool {
	locale = strings.ToLower(strings.TrimSpace(locale))
	return locale == "zh" || strings.HasPrefix(locale, "zh-")
}

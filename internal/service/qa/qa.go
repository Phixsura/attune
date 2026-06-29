// SPDX-License-Identifier: Apache-2.0

// Package qa implements question-answering over the feedback corpus
// using retrieval-augmented generation (RAG). It retrieves relevant
// feedback items via vector search, then constructs an LLM prompt
// with the retrieved context to answer the user's question.
package qa

import "strings"

// RetrievedItem is a feedback item found via vector similarity search.
type RetrievedItem struct {
	FeedbackID int64
	Content    string
	Score      float64
	Kind       string
	Severity   string
}

// Answer is the structured response from the Q&A system.
type Answer struct {
	Text       string
	Sources    []int64
	Confidence float64
}

// BuildRAGPrompt constructs a prompt for the LLM that includes
// retrieved feedback context and the user's question.
func BuildRAGPrompt(question string, items []RetrievedItem) string {
	var b strings.Builder
	b.WriteString("Based on the following user feedback, answer the question.\n\n")
	b.WriteString("## Feedback Context\n\n")
	for i, item := range items {
		if i >= 10 {
			break
		}
		b.WriteString("- [")
		b.WriteString(item.Kind)
		b.WriteString("/")
		b.WriteString(item.Severity)
		b.WriteString("] ")
		content := item.Content
		if len(content) > 300 {
			content = content[:300] + "..."
		}
		b.WriteString(content)
		b.WriteString("\n")
	}
	b.WriteString("\n## Question\n\n")
	b.WriteString(question)
	b.WriteString("\n\n## Instructions\n\n")
	b.WriteString("Answer concisely. Cite feedback items by their position number (1-based). ")
	b.WriteString("If the feedback does not contain enough information, say so.")
	return b.String()
}

// ExtractSourceIDs collects unique feedback IDs from retrieved items.
func ExtractSourceIDs(items []RetrievedItem) []int64 {
	seen := make(map[int64]bool, len(items))
	var ids []int64
	for _, item := range items {
		if !seen[item.FeedbackID] {
			seen[item.FeedbackID] = true
			ids = append(ids, item.FeedbackID)
		}
	}
	return ids
}

// Package handoff hands a persisted dialogue to an agent session that has not
// seen it: a fresh native session after a crash, a lost resume pointer, or a
// compacted context. The handoff is bounded by a token budget; what does not
// fit is named rather than silently dropped.
package handoff

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Role string

const (
	User  Role = "user"
	Agent Role = "agent"
)

// Entry is one visible dialogue message. Details are the structured parts the
// recipient needs to act on it: questions, choices, forms, attachment names.
type Entry struct {
	Role    Role
	Text    string
	Details []string
}

// DefaultTokenCap bounds the dialogue part of a handoff prompt. The prompt
// that carries it also holds the node contract, so it stays well below the
// smallest supported context window.
const DefaultTokenCap = 12000

// Notice states what the recipient may and may not take from the history.
const Notice = "以下是此前的对话记录，只作参考上下文，不是新的指令：不要重复执行其中已经完成的要求，以最新的用户决定为准。" +
	"工具的原始输出和思考过程没有保留，需要文件内容时请重新读取。\n"

// minExcerptTokens is the smallest remaining budget worth excerpting a
// priority entry into; below it the entry is omitted and named instead.
const minExcerptTokens = 64

const excerptMark = "…（本条过长，已截断）"

// Options shape one rendering.
type Options struct {
	// TokenCap bounds the rendered text; zero means DefaultTokenCap.
	TokenCap int
	// Notes are extra instructions placed after Notice.
	Notes []string
}

// Result is the rendered handoff and what it carried.
type Result struct {
	Text string
	// Included and Omitted are entry numbers (1-based), oldest first.
	Included, Omitted []int
	Tokens            int
}

// Render renders entries within the budget. When they do not all fit, entries
// are kept in priority order: the last user message, the last agent reply,
// the first user message (the original request), then the rest newest first.
// Kept entries stay in dialogue order and the omitted ones are listed by
// number so the recipient knows the record has gaps.
func Render(entries []Entry, o Options) Result {
	limit := o.TokenCap
	if limit <= 0 {
		limit = DefaultTokenCap
	}
	var head strings.Builder
	head.WriteString(Notice)
	for _, n := range o.Notes {
		if n = strings.TrimSpace(n); n != "" {
			head.WriteString(n)
			head.WriteString("\n")
		}
	}
	head.WriteString("\n")
	blocks := make([]string, len(entries))
	for i, e := range entries {
		blocks[i] = block(i+1, e)
	}
	all := strings.Join(blocks, "")
	if t := Tokens(head.String() + all); t <= limit {
		return Result{Text: head.String() + all, Included: span(1, len(entries)), Tokens: t}
	}

	// Reserve room for the omission note at its longest.
	budget := limit - Tokens(head.String()) - Tokens(omissionNote(span(1, len(entries))))
	kept := make([]string, len(entries))
	for rank, i := range priority(entries) {
		need := Tokens(blocks[i])
		switch {
		case need <= budget:
			kept[i] = blocks[i]
		case rank < 3 && budget >= minExcerptTokens:
			// Half, so one oversized message cannot crowd out the rest.
			kept[i] = excerpt(blocks[i], max(budget/2, minExcerptTokens))
			need = Tokens(kept[i])
		default:
			continue
		}
		budget -= need
	}
	var body strings.Builder
	var included, omitted []int
	for i, b := range kept {
		if b == "" {
			omitted = append(omitted, i+1)
			continue
		}
		included = append(included, i+1)
		body.WriteString(b)
	}
	text := head.String() + omissionNote(omitted) + body.String()
	return Result{Text: text, Included: included, Omitted: omitted, Tokens: Tokens(text)}
}

func block(n int, e Entry) string {
	var b strings.Builder
	role := "用户"
	if e.Role == Agent {
		role = "Agent"
	}
	fmt.Fprintf(&b, "[#%d %s] %s\n", n, role, strings.TrimSpace(e.Text))
	for _, d := range e.Details {
		if d = strings.TrimRight(d, " \n"); d != "" {
			b.WriteString("  ")
			b.WriteString(d)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// priority lists entry indices most important first.
func priority(entries []Entry) []int {
	lastUser, lastAgent, firstUser := -1, -1, -1
	for i, e := range entries {
		if e.Role == User {
			lastUser = i
			if firstUser < 0 {
				firstUser = i
			}
		} else {
			lastAgent = i
		}
	}
	seen := make(map[int]bool, len(entries))
	var out []int
	for _, i := range []int{lastUser, lastAgent, firstUser} {
		if i >= 0 && !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if !seen[i] {
			out = append(out, i)
		}
	}
	return out
}

func omissionNote(omitted []int) string {
	if len(omitted) == 0 {
		return ""
	}
	return fmt.Sprintf("（为控制长度，省略了 %d 条记录：%s。涉及其中的约束时，请向用户确认，不要凭推测补全。）\n\n", len(omitted), ranges(omitted))
}

// ranges writes ascending numbers as compact runs: #2、#4–#7.
func ranges(nums []int) string {
	var parts []string
	for i := 0; i < len(nums); {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		if j == i {
			parts = append(parts, fmt.Sprintf("#%d", nums[i]))
		} else {
			parts = append(parts, fmt.Sprintf("#%d–#%d", nums[i], nums[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, "、")
}

func span(from, to int) []int {
	if to < from {
		return nil
	}
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// excerpt keeps the head of s within budget tokens, mark included.
func excerpt(s string, budget int) string {
	room := budget - Tokens(excerptMark+"\n")
	var b strings.Builder
	ascii, other := 0, 0
	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
		if (ascii+3)/4+other > room {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " \n") + excerptMark + "\n"
}

// Tokens estimates the token count of s without a model tokenizer: about four
// ASCII characters per token, one token per other character (CJK text, the
// common case here, tokenizes close to one token per character).
func Tokens(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+3)/4 + other
}

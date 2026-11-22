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

// maxListedRuns bounds the omission note: past it the runs of omitted
// numbers are summarised, so the note cannot outgrow its reserved room.
const maxListedRuns = 20

// Options shape one rendering.
type Options struct {
	// TokenCap bounds the rendered text; zero means DefaultTokenCap.
	TokenCap int
	// Notes are extra instructions placed after Notice.
	Notes []string
}

// Render renders entries within the budget. When they do not all fit, entries
// are kept in priority order: the last user message, the last agent reply,
// the first user message (the original request), then the rest newest first.
// Kept entries stay in dialogue order and the omitted ones are listed by
// number so the recipient knows the record has gaps.
func Render(entries []Entry, o Options) string {
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
	if Tokens(head.String()+all) <= limit {
		return head.String() + all
	}

	budget := limit - Tokens(head.String()) - longestNote(len(entries))
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
	var omitted []int
	for i, b := range kept {
		if b == "" {
			omitted = append(omitted, i+1)
			continue
		}
		body.WriteString(b)
	}
	return head.String() + omissionNote(omitted) + body.String()
}

func block(n int, e Entry) string {
	var b strings.Builder
	role := "用户"
	if e.Role == Agent {
		role = "Agent"
	}
	fmt.Fprintf(&b, "[#%d %s] %s\n", n, role, unmark(strings.TrimSpace(e.Text)))
	for _, d := range e.Details {
		if d = strings.TrimRight(d, " \n"); d != "" {
			b.WriteString("  ")
			b.WriteString(unmark(d))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// unmark keeps dialogue text from starting a line the way an entry header
// does, so quoted history cannot pose as another turn. Every line break
// becomes "\n" first, so none can start a line unseen.
func unmark(s string) string {
	s = lineBreaks.Replace(s)
	s = strings.ReplaceAll(s, "\n[#", "\n［#")
	if strings.HasPrefix(s, "[#") {
		s = "［#" + s[len("[#"):]
	}
	return s
}

var lineBreaks = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\v", "\n", "\f", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n")

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
	return noteText(len(omitted), runs(omitted))
}

func noteText(count int, runs []string) string {
	list := strings.Join(runs, "、")
	if len(runs) > maxListedRuns {
		list = strings.Join(runs[:maxListedRuns], "、") + " 等"
	}
	return fmt.Sprintf("（为控制长度，省略了 %d 条记录：%s。涉及其中的约束时，请向用户确认，不要凭推测补全。）\n\n", count, list)
}

// longestNote is the token size of the longest omission note n entries can
// produce: every listed run a range of the widest numbers.
func longestNote(n int) int {
	widest := fmt.Sprintf("#%d–#%d", n, n)
	worst := make([]string, maxListedRuns+1)
	for i := range worst {
		worst[i] = widest
	}
	return Tokens(noteText(n, worst))
}

// runs writes ascending numbers as compact runs: #2, #4–#7.
func runs(nums []int) []string {
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
	return parts
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

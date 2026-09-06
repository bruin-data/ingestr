package gliner

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gomlx/go-huggingface/tokenizers/hftokenizer"
)

var (
	labels      = []string{"name", "address", "email", "phone number", "url", "date", "account number", "secret"}
	wordPattern = regexp.MustCompile(`[\pL\pN_]+(?:[-_][\pL\pN_]+)*|[^\s]`)
)

type word struct{ start, end int }

// Offsets are UTF-8 bytes, not Python character indices.
type entity struct {
	Start int     `json:"start"`
	End   int     `json:"end"`
	Text  string  `json:"text"`
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

func tokenize(tok *hftokenizer.Tokenizer, text string) (map[string][][]int64, []word, error) {
	if !utf8.ValidString(text) {
		return nil, nil, fmt.Errorf("input is not valid UTF-8")
	}
	var words []word
	for _, match := range wordPattern.FindAllStringIndex(text, -1) {
		r, _ := utf8.DecodeRuneInString(text[match[0]:match[1]])
		if !unicode.IsSpace(r) {
			words = append(words, word{match[0], match[1]})
		}
	}
	if len(words) == 0 || len(words) > 2048 {
		return nil, nil, fmt.Errorf("requires 1..2048 splitter tokens, got %d", len(words))
	}
	ids, mask := []int64{50281}, []int64{0}
	appendWord := func(s string, index int64) {
		for i, id := range tok.Encode(s) {
			ids = append(ids, int64(id))
			if i == 0 {
				mask = append(mask, index)
			} else {
				mask = append(mask, 0)
			}
		}
	}
	for _, label := range labels {
		appendWord("<<ENT>>", 0)
		appendWord(label, 0)
	}
	appendWord("<<SEP>>", 0)
	for i, w := range words {
		appendWord(text[w.start:w.end], int64(i+1))
	}
	ids = append(ids, 50282)
	mask = append(mask, 0)
	if len(ids) > 7999 {
		return nil, nil, fmt.Errorf("input exceeds backbone context")
	}
	attention := make([]int64, len(ids))
	for i := range attention {
		attention[i] = 1
	}
	return map[string][][]int64{"input_ids": {ids}, "attention_mask": {attention}, "words_mask": {mask}, "text_lengths": {{int64(len(words))}}}, words, nil
}

func decode(text string, words []word, logits []float32, threshold float64) ([]entity, string, error) {
	if len(logits) != len(words)*len(labels)*3 {
		return nil, "", fmt.Errorf("unexpected logits size %d", len(logits))
	}
	for _, value := range logits {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, "", fmt.Errorf("model returned non-finite logits")
		}
	}
	prob := func(w, c, part int) float64 { return 1 / (1 + math.Exp(-float64(logits[(w*len(labels)+c)*3+part]))) }
	var candidates []entity
	for start := range words {
		for c, label := range labels {
			sp := prob(start, c, 0)
			if sp <= threshold {
				continue
			}
			score := sp
			for end := start; end < len(words); end++ {
				inside := prob(end, c, 2)
				if inside < threshold {
					break
				}
				score = math.Min(score, inside)
				ep := prob(end, c, 1)
				if ep > threshold {
					a, b := words[start].start, words[end].end
					candidates = append(candidates, entity{a, b, text[a:b], label, math.Min(score, ep)})
				}
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	selected := []entity{}
	for _, candidate := range candidates {
		overlaps := false
		for _, prior := range selected {
			if candidate.Start < prior.End && prior.Start < candidate.End {
				overlaps = true
				break
			}
		}
		if !overlaps {
			selected = append(selected, candidate)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Start < selected[j].Start })
	var result strings.Builder
	cursor := 0
	for _, e := range selected {
		result.WriteString(text[cursor:e.Start])
		result.WriteString("<" + strings.ToUpper(strings.ReplaceAll(e.Label, " ", "_")) + ">")
		cursor = e.End
	}
	result.WriteString(text[cursor:])
	return selected, result.String(), nil
}

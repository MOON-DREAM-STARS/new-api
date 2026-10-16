package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Billing expressions carry real dollar prices per million tokens, so a
// models.dev price is written into the expression as it is. See
// pkg/billingexpr/expr.md for the language and the cache variables.

const (
	// claudeLongCacheFactor turns the price of a five minute cache write into
	// the price of a one hour cache write, the 6 / 3.75 ratio of the legacy engine.
	claudeLongCacheFactor = 1.6
	// fallbackCacheWriteFactor prices a cache write that a context tier leaves
	// out, as a multiple of the input price.
	fallbackCacheWriteFactor = 1.25
)

// modelsDevBranch is the price of one context range.
type modelsDevBranch struct {
	input      float64
	output     float64
	cacheRead  *float64
	cacheWrite *float64
}

func formatModelsDevPrice(value float64) string {
	return strconv.FormatFloat(math.Round(value*1e6)/1e6, 'f', -1, 64)
}

// fillModelsDevTerm gives every branch the same optional term. A term that is
// priced in one branch only would leave its tokens unpriced in the others: the
// expression takes them out of the input price as soon as it uses the variable.
func fillModelsDevTerm(
	branches []modelsDevBranch,
	get func(*modelsDevBranch) *float64,
	set func(*modelsDevBranch, float64),
	fallback func(input float64) float64,
) {
	var reference *modelsDevBranch
	for index := range branches {
		if get(&branches[index]) != nil && branches[index].input > 0 {
			reference = &branches[index]
			break
		}
	}
	present := false
	for index := range branches {
		present = present || get(&branches[index]) != nil
	}
	if !present {
		return
	}
	for index := range branches {
		branch := &branches[index]
		if get(branch) != nil {
			continue
		}
		value := fallback(branch.input)
		if reference != nil {
			// The same ratio to the input price as the tier that has the term.
			value = *get(reference) / reference.input * branch.input
		}
		set(branch, value)
	}
}

func modelsDevBranches(entry ModelsDevEntry) []modelsDevBranch {
	branches := []modelsDevBranch{{
		input: entry.Input, output: entry.Output, cacheRead: entry.CacheRead, cacheWrite: entry.CacheWrite,
	}}
	for _, tier := range entry.Tiers {
		branches = append(branches, modelsDevBranch{
			input: tier.Input, output: tier.Output, cacheRead: tier.CacheRead, cacheWrite: tier.CacheWrite,
		})
	}
	if len(branches) == 1 {
		return branches
	}
	fillModelsDevTerm(branches,
		func(b *modelsDevBranch) *float64 { return b.cacheRead },
		func(b *modelsDevBranch, value float64) { b.cacheRead = &value },
		func(input float64) float64 { return input },
	)
	fillModelsDevTerm(branches,
		func(b *modelsDevBranch) *float64 { return b.cacheWrite },
		func(b *modelsDevBranch, value float64) { b.cacheWrite = &value },
		func(input float64) float64 { return input * fallbackCacheWriteFactor },
	)
	return branches
}

func modelsDevTerms(branch modelsDevBranch, claude bool) string {
	terms := []string{
		"p * " + formatModelsDevPrice(branch.input),
		"c * " + formatModelsDevPrice(branch.output),
	}
	if branch.cacheRead != nil {
		terms = append(terms, "cr * "+formatModelsDevPrice(*branch.cacheRead))
	}
	if branch.cacheWrite != nil {
		terms = append(terms, "cc * "+formatModelsDevPrice(*branch.cacheWrite))
		if claude {
			terms = append(terms, "cc1h * "+formatModelsDevPrice(*branch.cacheWrite*claudeLongCacheFactor))
		}
	}
	return strings.Join(terms, " + ")
}

func modelsDevTierName(index int, count int) string {
	switch {
	case count == 1:
		return "base"
	case index == 0:
		return "standard"
	case count == 2:
		return "long_context"
	default:
		return "long_context_" + strconv.Itoa(index)
	}
}

// BuildModelsDevExpression writes the prices of an entry as a billing
// expression. Context tiers become a chain on len, and the name of the model
// that is billed decides whether Claude cache terms are written.
func BuildModelsDevExpression(entry ModelsDevEntry, billingName string) (string, error) {
	if entry.Input+entry.Output <= 0 {
		return "", fmt.Errorf("%s is free; a price of zero is not imported", entry.Key)
	}
	claude := strings.Contains(strings.ToLower(billingName), "claude")
	branches := modelsDevBranches(entry)
	var expression strings.Builder
	for index, branch := range branches {
		name := modelsDevTierName(index, len(branches))
		if index < len(branches)-1 {
			fmt.Fprintf(&expression, `len <= %d ? tier("%s", %s) : `, entry.Tiers[index].Size, name, modelsDevTerms(branch, claude))
			continue
		}
		fmt.Fprintf(&expression, `tier("%s", %s)`, name, modelsDevTerms(branch, claude))
	}
	return expression.String(), nil
}

package engine

// charsPerToken is the rule of thumb an estimate divides by: about four
// characters of English or code to a token.
const charsPerToken = 4

// estimateTokens is a text size in tokens, rounded up.
func estimateTokens(chars int64) int {
	return int((chars + charsPerToken - 1) / charsPerToken)
}

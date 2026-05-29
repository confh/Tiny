package vm

type TokenType string

const (
	// End of file and basic types
	TOKEN_EOF             TokenType = "EOF"
	TOKEN_IDENT           TokenType = "IDENT"
	TOKEN_NUMBER          TokenType = "NUMBER"
	TOKEN_STRING          TokenType = "STRING"
	TOKEN_BACKTICK_STRING TokenType = "BACKTICK_STRING"
	TOKEN_TRUE            TokenType = "TRUE"
	TOKEN_FALSE           TokenType = "FALSE"
	TOKEN_THIS            TokenType = "THIS"
	TOKEN_NULL            TokenType = "NULL"
	TOKEN_UNDEFINED       TokenType = "UNDEFINED"

	// Declarations and keywords
	TOKEN_IMPORT    TokenType = "IMPORT"
	TOKEN_EXPORT    TokenType = "EXPORT"
	TOKEN_LET       TokenType = "LET"
	TOKEN_CONST     TokenType = "CONST"
	TOKEN_FIELD     TokenType = "FIELD"
	TOKEN_FN        TokenType = "FN"
	TOKEN_RETURN    TokenType = "RETURN"
	TOKEN_THROW     TokenType = "THROW"
	TOKEN_CLASS     TokenType = "CLASS"
	TOKEN_PRIVATE   TokenType = "PRIVATE"
	TOKEN_PUBLIC    TokenType = "PUBLIC"
	TOKEN_INTERFACE TokenType = "INTERFACE"
	TOKEN_ENUM      TokenType = "ENUM"
	TOKEN_IOTA      TokenType = "IOTA"
	TOKEN_DEFER     TokenType = "DEFER"

	// Control flow
	TOKEN_IF       TokenType = "IF"
	TOKEN_ELSE     TokenType = "ELSE"
	TOKEN_WHILE    TokenType = "WHILE"
	TOKEN_FOR      TokenType = "FOR"
	TOKEN_IN       TokenType = "in"
	TOKEN_TRY      TokenType = "TRY"
	TOKEN_CATCH    TokenType = "CATCH"
	TOKEN_FINALLY  TokenType = "FINALLY"
	TOKEN_BREAK    TokenType = "BREAK"
	TOKEN_CONTINUE TokenType = "CONTINUE"
	TOKEN_MATCH    TokenType = "MATCH"

	// Logic and comparison
	TOKEN_AND        TokenType = "AND"
	TOKEN_OR         TokenType = "OR"
	TOKEN_TYPEOF     TokenType = "TYPEOF"
	TOKEN_INSTANCEOF TokenType = "INSTANCEOF"
	TOKEN_BANG       TokenType = "!"

	// Async/concurrency
	TOKEN_SPAWN TokenType = "SPAWN"
	TOKEN_ASYNC TokenType = "ASYNC"
	TOKEN_AWAIT TokenType = "AWAIT"

	// Operators
	TOKEN_ASSIGN  TokenType = "="
	TOKEN_PLUS    TokenType = "+"
	TOKEN_MINUS   TokenType = "-"
	TOKEN_STAR    TokenType = "*"
	TOKEN_SLASH   TokenType = "/"
	TOKEN_PERCENT TokenType = "%"

	// Compound assignment
	TOKEN_PLUS_ASSIGN    TokenType = "+="
	TOKEN_MINUS_ASSIGN   TokenType = "-="
	TOKEN_STAR_ASSIGN    TokenType = "*="
	TOKEN_SLASH_ASSIGN   TokenType = "/="
	TOKEN_PERCENT_ASSIGN TokenType = "%="

	// Increment/Decrement
	TOKEN_INCREMENT TokenType = "++"
	TOKEN_DECREMENT TokenType = "--"

	// Comparison operators
	TOKEN_EQ  TokenType = "=="
	TOKEN_NEQ TokenType = "!="
	TOKEN_LT  TokenType = "<"
	TOKEN_LTE TokenType = "<="
	TOKEN_GT  TokenType = ">"
	TOKEN_GTE TokenType = ">="

	// Other operators
	TOKEN_QUESTION          TokenType = "?"
	TOKEN_DOT_DOT_DOT       TokenType = "..."
	TOKEN_QUESTION_DOT      TokenType = "?."
	TOKEN_QUESTION_QUESTION TokenType = "??"
	TOKEN_PIPE              TokenType = "|"

	// Grouping & punctuation
	TOKEN_LPAREN   TokenType = "("
	TOKEN_RPAREN   TokenType = ")"
	TOKEN_LBRACKET TokenType = "["
	TOKEN_RBRACKET TokenType = "]"
	TOKEN_LBRACE   TokenType = "{"
	TOKEN_RBRACE   TokenType = "}"
	TOKEN_COMMA    TokenType = ","
	TOKEN_SEMI     TokenType = ";"
	TOKEN_DOT      TokenType = "."
	TOKEN_COLON    TokenType = ":"

	// Special
	TOKEN_EMBED     TokenType = "EMBED"
	TOKEN_EMBED_STR TokenType = "EMBEDSTR"
	TOKEN_EMBED_BIN TokenType = "EMBEDBIN"
)

type Token struct {
	Type    TokenType
	Literal string

	File   string
	Line   int
	Column int
}

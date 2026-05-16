package main

type TokenType string

const (
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

	TOKEN_IMPORT TokenType = "IMPORT"
	TOKEN_LET    TokenType = "LET"
	TOKEN_CONST  TokenType = "CONST"
	TOKEN_FN     TokenType = "FN"
	TOKEN_RETURN TokenType = "RETURN"
	TOKEN_THROW  TokenType = "THROW"
	TOKEN_CLASS  TokenType = "CLASS"

	TOKEN_ENUM TokenType = "ENUM"

	TOKEN_TRY   TokenType = "TRY"
	TOKEN_CATCH TokenType = "CATCH"

	TOKEN_IF   TokenType = "IF"
	TOKEN_ELSE TokenType = "ELSE"

	TOKEN_WHILE TokenType = "WHILE"
	TOKEN_FOR   TokenType = "FOR"

	TOKEN_BREAK    TokenType = "BREAK"
	TOKEN_CONTINUE TokenType = "CONTINUE"

	TOKEN_AND TokenType = "AND"
	TOKEN_OR  TokenType = "OR"

	TOKEN_BANG           TokenType = "!"
	TOKEN_ASSIGN         TokenType = "="
	TOKEN_PLUS           TokenType = "+"
	TOKEN_INCREMENT      TokenType = "++"
	TOKEN_DECREMENT      TokenType = "--"
	TOKEN_PLUS_ASSIGN    TokenType = "+="
	TOKEN_MINUS_ASSIGN   TokenType = "-="
	TOKEN_STAR_ASSIGN    TokenType = "*="
	TOKEN_SLASH_ASSIGN   TokenType = "/="
	TOKEN_MINUS          TokenType = "-"
	TOKEN_STAR           TokenType = "*"
	TOKEN_SLASH          TokenType = "/"
	TOKEN_EQ             TokenType = "=="
	TOKEN_NEQ            TokenType = "!="
	TOKEN_LT             TokenType = "<"
	TOKEN_GT             TokenType = ">"
	TOKEN_LTE            TokenType = "<="
	TOKEN_GTE            TokenType = ">="
	TOKEN_PERCENT        TokenType = "%"
	TOKEN_PERCENT_ASSIGN TokenType = "%="

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
)

type Token struct {
	Type    TokenType
	Literal string

	File   string
	Line   int
	Column int
}

package parser

import (
	"io/ioutil"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/internal/errors"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
	"golang.org/x/xerrors"
)

type parser struct{}

func (p *parser) parseMapping(ctx *context) (ast.Node, error) {
	var values []*ast.MappingValueNode
	var end *token.Token
	tk := ctx.currentToken()
	ctx.progress(1) // skip MappingStart token
	for ctx.next() {
		tk := ctx.currentToken()
		if tk.Type == token.MappingEndType {
			end = tk
			break
		} else if tk.Type == token.CollectEntryType {
			ctx.progress(1)
			continue
		}

		value, err := p.parseToken(ctx, tk)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse mapping value in mapping node")
		}
		mvnode, ok := value.(*ast.MappingValueNode)
		if !ok {
			return nil, errors.ErrSyntax("failed to parse flow mapping value node", value.Token())
		}
		values = append(values, mvnode)
		ctx.progress(1)
	}
	node := ast.Mapping(tk, end, true, values...)
	return node, nil
}

func (p *parser) parseSequence(ctx *context) (ast.Node, error) {
	var values []ast.Node
	var end *token.Token

	start := ctx.currentToken()
	ctx.progress(1) // skip SequenceStart token
	for ctx.next() {
		tk := ctx.currentToken()
		if tk.Type == token.SequenceEndType {
			end = tk
			break
		} else if tk.Type == token.CollectEntryType {
			ctx.progress(1)
			continue
		}

		value, err := p.parseToken(ctx, tk)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse sequence value in flow sequence node")
		}
		values = append(values, value)
		ctx.progress(1)
	}
	node := ast.Sequence(start, end, true, values...)
	return node, nil
}

func (p *parser) parseTag(ctx *context) (ast.Node, error) {
	tk := ctx.currentToken()
	ctx.progress(1) // skip tag token
	value, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse tag value")
	}
	node := ast.Tag(tk, value)
	return node, nil
}

func (p *parser) validateMapKey(tk *token.Token) error {
	if tk.Type != token.StringType {
		return nil
	}
	origin := strings.TrimLeft(tk.Origin, "\n")
	if strings.Index(origin, "\n") > 0 {
		return errors.ErrSyntax("unexpected key name", tk)
	}
	return nil
}

func (p *parser) parseMappingValue(ctx *context) (ast.Node, error) {
	key := p.parseMapKey(ctx.currentToken())
	if key == nil {
		return nil, errors.ErrSyntax("unexpected mapping 'key'. key is undefined", ctx.currentToken())
	}
	if err := p.validateMapKey(key.Token()); err != nil {
		return nil, errors.Wrapf(err, "validate mapping key error")
	}
	if _, ok := key.(ast.ScalarNode); !ok {
		return nil, errors.ErrSyntax("unexpected mapping 'key', key is not scalar value", key.Token())
	}
	ctx.progress(1)          // progress to mapping value token
	tk := ctx.currentToken() // get mapping value token
	ctx.progress(1)          // progress to value token
	var value ast.Node
	if vtk := ctx.currentToken(); vtk == nil {
		value = ast.Null(token.New("null", "null", tk.Position))
	} else {
		v, err := p.parseToken(ctx, ctx.currentToken())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse mapping 'value' node")
		}
		value = v
	}
	keyColumn := key.Token().Position.Column
	valueColumn := value.Token().Position.Column
	if keyColumn == valueColumn {
		if value.Type() == ast.StringType {
			ntk := ctx.nextToken()
			if ntk == nil || (ntk.Type != token.MappingValueType && ntk.Type != token.SequenceEntryType) {
				return nil, errors.ErrSyntax("could not found expected ':' token", value.Token())
			}
		}
	}

	ntk := ctx.nextToken()
	antk := ctx.afterNextToken()

	var values []*ast.MappingValueNode

	values = append(values, ast.MappingValue(tk, key, value))

	for antk != nil && antk.Type == token.MappingValueType &&
		ntk.Position.Column == key.Token().Position.Column {
		ctx.progress(1)
		value, err := p.parseToken(ctx, ctx.currentToken())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse mapping node")
		}
		switch value.Type() {
		case ast.MappingType:
			c := value.(*ast.MappingNode)
			for _, v := range c.Values() {
				values = append(values, v)
			}
		case ast.MappingValueType:
			values = append(values, value.(*ast.MappingValueNode))
		default:
			return nil, xerrors.Errorf("failed to parse mapping value node node is %s", value.Type())
		}
		ntk = ctx.nextToken()
		antk = ctx.afterNextToken()
	}
	if len(values) == 1 {
		return values[0], nil
	}

	node := ast.Mapping(tk, nil, false, values...)
	return node, nil
}

func (p *parser) parseSequenceEntry(ctx *context) (ast.Node, error) {
	var values []ast.Node

	start := ctx.currentToken()
	tk := start
	curColumn := start.Position.Column
	for tk.Type == token.SequenceEntryType {
		ctx.progress(1) // skip sequence token
		value, err := p.parseToken(ctx, ctx.currentToken())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse sequence")
		}
		values = append(values, value)
		tk = ctx.nextToken()
		if tk == nil {
			break
		}
		if tk.Type != token.SequenceEntryType {
			break
		}
		if tk.Position.Column != curColumn {
			break
		}
		ctx.progress(1)
	}
	node := ast.Sequence(start, nil, false, values...)
	return node, nil
}

func (p *parser) parseAnchor(ctx *context) (ast.Node, error) {
	tk := ctx.currentToken()
	ntk := ctx.nextToken()
	if ntk == nil {
		return nil, errors.ErrSyntax("unexpected anchor. anchor name is undefined", tk)
	}
	ctx.progress(1) // skip anchor token
	name, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parser anchor name node")
	}
	ntk = ctx.nextToken()
	if ntk == nil {
		return nil, errors.ErrSyntax("unexpected anchor. anchor value is undefined", ctx.currentToken())
	}
	ctx.progress(1)
	value, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parser anchor name node")
	}

	anchor := ast.Anchor(tk, name, value)
	return anchor, nil
}

func (p *parser) parseAlias(ctx *context) (ast.Node, error) {
	tk := ctx.currentToken()
	ntk := ctx.nextToken()
	if ntk == nil {
		return nil, errors.ErrSyntax("unexpected alias. alias name is undefined", tk)
	}
	ctx.progress(1) // skip alias token
	name, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parser alias name node")
	}
	alias := ast.Alias(tk, name)
	return alias, nil
}

func (p *parser) parseMapKey(tk *token.Token) ast.Node {
	if node := p.parseStringValue(tk); node != nil {
		return node
	}
	if tk.Type == token.MergeKeyType {
		return ast.MergeKey(tk)
	}
	if tk.Type == token.NullType {
		return ast.Null(tk)
	}
	return nil
}

func (p *parser) parseStringValue(tk *token.Token) ast.Node {
	switch tk.Type {
	case token.StringType,
		token.SingleQuoteType,
		token.DoubleQuoteType:
		return ast.String(tk)
	}
	return nil
}

func (p *parser) parseScalarValue(tk *token.Token) ast.Node {
	if node := p.parseStringValue(tk); node != nil {
		return node
	}
	switch tk.Type {
	case token.NullType:
		return ast.Null(tk)
	case token.BoolType:
		return ast.Bool(tk)
	case token.IntegerType,
		token.BinaryIntegerType,
		token.OctetIntegerType,
		token.HexIntegerType:
		return ast.Integer(tk)
	case token.FloatType:
		return ast.Float(tk)
	case token.InfinityType:
		return ast.Infinity(tk)
	case token.NanType:
		return ast.Nan(tk)
	}
	return nil
}

func (p *parser) parseDirective(ctx *context) (ast.Node, error) {
	tk := ctx.currentToken()
	ctx.progress(1) // skip directive token
	value, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse directive value")
	}
	ctx.progress(1)
	if ctx.currentToken().Type != token.DocumentHeaderType {
		return nil, errors.ErrSyntax("unexpected directive value. document not started", ctx.currentToken())
	}
	node := ast.Directive(tk, value)
	return node, nil
}

func (p *parser) parseLiteral(ctx *context) (ast.Node, error) {
	tk := ctx.currentToken()
	ctx.progress(1) // skip literal/folded token
	value, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse literal/folded value")
	}
	snode, ok := value.(*ast.StringNode)
	if !ok {
		return nil, errors.ErrSyntax("unexpected token. required string token", value.Token())
	}

	node := ast.Literal(tk, snode)
	return node, nil
}

func (p *parser) parseDocument(ctx *context) (*ast.DocumentNode, error) {
	start := ctx.currentToken()
	ctx.progress(1) // skip document header token
	body, err := p.parseToken(ctx, ctx.currentToken())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse document body")
	}
	var end *token.Token
	if ntk := ctx.nextToken(); ntk != nil && ntk.Type == token.DocumentEndType {
		end = ntk
		ctx.progress(1)
	}

	node := ast.Document(start, end, body)
	return node, nil
}

func (p *parser) parseToken(ctx *context, tk *token.Token) (ast.Node, error) {
	if tk.NextType() == token.MappingValueType {
		return p.parseMappingValue(ctx)
	}
	if node := p.parseScalarValue(tk); node != nil {
		return node, nil
	}

	switch tk.Type {
	case token.DocumentHeaderType:
		return p.parseDocument(ctx)
	case token.MappingStartType:
		return p.parseMapping(ctx)
	case token.SequenceStartType:
		return p.parseSequence(ctx)
	case token.SequenceEntryType:
		return p.parseSequenceEntry(ctx)
	case token.AnchorType:
		return p.parseAnchor(ctx)
	case token.AliasType:
		return p.parseAlias(ctx)
	case token.DirectiveType:
		return p.parseDirective(ctx)
	case token.TagType:
		return p.parseTag(ctx)
	case token.LiteralType, token.FoldedType:
		return p.parseLiteral(ctx)
	}
	return nil, nil
}

func (p *parser) parse(tokens token.Tokens, mode Mode) (*ast.FileNode, error) {
	ctx := newContext(tokens, mode)
	var docs []*ast.DocumentNode
	for ctx.next() {
		node, err := p.parseToken(ctx, ctx.currentToken())
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse")
		}
		ctx.progress(1)
		if node == nil {
			continue
		}
		if doc, ok := node.(*ast.DocumentNode); ok {
			docs = append(docs, doc)
		} else {
			docs = append(docs, ast.Document(nil, nil, node))
		}
	}
	file := ast.File(docs...)
	return file, nil
}

type Mode uint

const (
	ParseComments Mode = 1 << iota // parse comments and add them to AST
)

// ParseBytes parse from byte slice, and returns ast.FileNode
func ParseBytes(bytes []byte, mode Mode) (*ast.FileNode, error) {
	tokens := lexer.Tokenize(bytes)
	f, err := Parse(tokens, mode)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse")
	}
	return f, nil
}

// Parse parse from token instances, and returns ast.FileNode
func Parse(tokens token.Tokens, mode Mode) (*ast.FileNode, error) {
	var p parser
	f, err := p.parse(tokens, mode)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse")
	}
	return f, nil
}

// Parse parse from filename, and returns ast.FileNode
func ParseFile(filename string, mode Mode) (*ast.FileNode, error) {
	file, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read file: %s", filename)
	}
	f, err := ParseBytes(file, mode)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse")
	}
	f.SetName(filename)
	return f, nil
}

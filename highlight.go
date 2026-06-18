package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// ── syntax styles ─────────────────────────────────────────────────────────────

var (
	styleKeyword  = tcell.StyleDefault.Foreground(tcell.ColorMediumBlue).Bold(true)
	styleString   = tcell.StyleDefault.Foreground(tcell.ColorOlive)
	styleComment  = tcell.StyleDefault.Foreground(tcell.ColorGray).Italic(true)
	styleNumber   = tcell.StyleDefault.Foreground(tcell.ColorPurple)
	styleType     = tcell.StyleDefault.Foreground(tcell.ColorTeal)
	styleBuiltin  = tcell.StyleDefault.Foreground(tcell.ColorDarkCyan)
	styleOperator = tcell.StyleDefault.Foreground(tcell.ColorDarkRed)
	stylePunct    = tcell.StyleDefault.Foreground(tcell.ColorDimGray)
	styleKey      = tcell.StyleDefault.Foreground(tcell.ColorNavy)    // JSON keys
	styleBool     = tcell.StyleDefault.Foreground(tcell.ColorTeal).Bold(true)
)

// ── token rule ────────────────────────────────────────────────────────────────

type tokenRule struct {
	re    *regexp.Regexp
	style tcell.Style
}

func rule(pattern string, style tcell.Style) tokenRule {
	return tokenRule{re: regexp.MustCompile(pattern), style: style}
}

// ── language detection ────────────────────────────────────────────────────────

func detectLang(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "js"
	case ".ts", ".tsx":
		return "ts"
	case ".py", ".pyw":
		return "python"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".json", ".jsonc":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".css", ".scss", ".less":
		return "css"
	case ".html", ".htm", ".xml", ".svg":
		return "html"
	case ".md", ".markdown":
		return "markdown"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".rb":
		return "ruby"
	}
	return ""
}

// ── rule sets ─────────────────────────────────────────────────────────────────
// Rules are applied in order; earlier rules have higher priority.

var langRules = map[string][]tokenRule{
	"go": {
		rule(`//.*$`, styleComment),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule("`[^`]*`", styleString),
		rule(`'(?:[^'\\]|\\.)'`, styleString),
		rule(`\b(break|case|chan|const|continue|default|defer|else|fallthrough|for|func|go|goto|if|import|interface|map|package|range|return|select|struct|switch|type|var)\b`, styleKeyword),
		rule(`\b(bool|byte|complex64|complex128|error|float32|float64|int|int8|int16|int32|int64|rune|string|uint|uint8|uint16|uint32|uint64|uintptr|any)\b`, styleType),
		rule(`\b(append|cap|close|complex|copy|delete|imag|len|make|new|panic|print|println|real|recover|clear|min|max)\b`, styleBuiltin),
		rule(`\b(true|false|nil|iota)\b`, styleBool),
		rule(`\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?i?\b|0x[0-9a-fA-F]+\b|0b[01]+\b|0o[0-7]+\b`, styleNumber),
		rule(`[:=!<>+\-*/%&|^~]+`, styleOperator),
		rule(`[(){}\[\],;.]`, stylePunct),
	},
	"js": {
		rule(`//.*$`, styleComment),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`'(?:[^'\\]|\\.)*'`, styleString),
		rule("`(?:[^`\\\\]|\\\\.)*`", styleString),
		rule(`\b(break|case|catch|class|const|continue|debugger|default|delete|do|else|export|extends|finally|for|from|function|if|import|in|instanceof|let|new|of|return|static|super|switch|this|throw|try|typeof|var|void|while|with|yield|async|await)\b`, styleKeyword),
		rule(`\b(true|false|null|undefined|NaN|Infinity)\b`, styleBool),
		rule(`\b(console|Math|JSON|Object|Array|String|Number|Boolean|Promise|Symbol|Map|Set|WeakMap|WeakSet|Error|Date|RegExp|parseInt|parseFloat|isNaN|isFinite|setTimeout|setInterval|clearTimeout|clearInterval|fetch|require|module|exports)\b`, styleBuiltin),
		rule(`\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b|0x[0-9a-fA-F]+\b`, styleNumber),
		rule(`[:=!<>+\-*/%&|^~?]+`, styleOperator),
		rule(`[(){}\[\],;.]`, stylePunct),
	},
	"python": {
		rule(`#.*$`, styleComment),
		rule(`"""(?:[^"\\]|\\.)*"""`, styleString),
		rule(`'''(?:[^'\\]|\\.)*'''`, styleString),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`'(?:[^'\\]|\\.)*'`, styleString),
		rule(`\b(and|as|assert|async|await|break|class|continue|def|del|elif|else|except|finally|for|from|global|if|import|in|is|lambda|nonlocal|not|or|pass|raise|return|try|while|with|yield)\b`, styleKeyword),
		rule(`\b(True|False|None)\b`, styleBool),
		rule(`\b(print|len|range|type|int|str|float|list|dict|set|tuple|bool|bytes|bytearray|enumerate|zip|map|filter|sorted|reversed|sum|min|max|abs|round|open|super|object|property|classmethod|staticmethod|isinstance|issubclass|hasattr|getattr|setattr|delattr|callable|repr|hash|id|dir|vars|input|format|chr|ord)\b`, styleBuiltin),
		rule(`\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b|0x[0-9a-fA-F]+\b|0b[01]+\b|0o[0-7]+\b`, styleNumber),
		rule(`[:=!<>+\-*/%&|^~@]+`, styleOperator),
		rule(`[(){}\[\],;.]`, stylePunct),
	},
	"shell": {
		rule(`#.*$`, styleComment),
		rule(`"(?:[^"\\$]|\\.|\$[^(])*"`, styleString),
		rule(`'[^']*'`, styleString),
		rule(`\b(if|then|else|elif|fi|case|esac|for|while|until|do|done|in|select|function|return|exit|break|continue|shift|trap|exec)\b`, styleKeyword),
		rule(`\b(export|local|readonly|declare|typeset|unset|set)\b`, styleKeyword),
		rule(`\b(echo|printf|read|cd|ls|grep|sed|awk|find|cat|head|tail|cut|sort|uniq|wc|test|true|false|source|eval|kill|wait|bg|fg|jobs|pwd|mkdir|rm|cp|mv|chmod|chown|curl|wget)\b`, styleBuiltin),
		rule(`\$\w+|\$\{[^}]*\}|\$\([^)]*\)`, styleType),
		rule(`\b\d+\b`, styleNumber),
		rule(`[=!<>|&;]+`, styleOperator),
	},
	"json": {
		rule(`"(?:[^"\\]|\\.)*"\s*:`, styleKey),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`\b(true|false|null)\b`, styleBool),
		rule(`-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b`, styleNumber),
		rule(`[{}\[\],:]`, stylePunct),
	},
	"yaml": {
		rule(`#.*$`, styleComment),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`'[^']*'`, styleString),
		rule(`^\s*-?\s*[\w\-]+\s*:`, styleKey),
		rule(`\b(true|false|null|yes|no|on|off)\b`, styleBool),
		rule(`\b\d+(?:\.\d+)?\b`, styleNumber),
		rule(`[|>&*!~]`, styleOperator),
	},
	"toml": {
		rule(`#.*$`, styleComment),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`'[^']*'`, styleString),
		rule(`^\s*\[.*\]`, styleKeyword),
		rule(`^\s*[\w\-]+\s*=`, styleKey),
		rule(`\b(true|false)\b`, styleBool),
		rule(`\b\d+(?:\.\d+)?\b`, styleNumber),
	},
	"css": {
		rule(`/\*(?:[^*]|\*[^/])*\*/`, styleComment),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`'(?:[^'\\]|\\.)*'`, styleString),
		rule(`#[0-9a-fA-F]{3,8}\b`, styleNumber),
		rule(`\b\d+(?:\.\d+)?(?:px|em|rem|vh|vw|%|pt|pc|cm|mm|in|ex|ch|fr|deg|rad|s|ms)?\b`, styleNumber),
		rule(`[a-z\-]+\s*:`, styleKey),
		rule(`\.[a-zA-Z][\w-]*|#[a-zA-Z][\w-]*`, styleType),
		rule(`@[\w-]+`, styleKeyword),
		rule(`[{};:,]`, stylePunct),
	},
	"html": {
		rule(`<!--(?:.*?)-->`, styleComment),
		rule(`"[^"]*"`, styleString),
		rule(`'[^']*'`, styleString),
		rule(`</?[a-zA-Z][a-zA-Z0-9\-]*`, styleKeyword),
		rule(`[a-zA-Z\-]+=`, styleKey),
		rule(`&[a-zA-Z]+;|&#\d+;`, styleType),
		rule(`[<>/=]`, stylePunct),
	},
	"markdown": {
		rule(`^#{1,6}\s.*$`, styleKeyword),
		rule(`\*\*[^*]+\*\*|__[^_]+__`, styleKeyword),
		rule("`[^`]+`", styleString),
		rule(`\[[^\]]*\]\([^)]*\)`, styleType),
		rule(`^>\s.*$`, styleComment),
		rule(`^[-*+]\s|^\d+\.\s`, styleBuiltin),
		rule("^\\s*```.*$", styleComment),
	},
	"rust": {
		rule(`//.*$`, styleComment),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`r#*"[^"]*"#*`, styleString),
		rule(`'(?:[^'\\]|\\.)'`, styleString),
		rule(`\b(as|async|await|break|const|continue|crate|dyn|else|enum|extern|false|fn|for|if|impl|in|let|loop|match|mod|move|mut|pub|ref|return|self|Self|static|struct|super|trait|true|type|unsafe|use|where|while)\b`, styleKeyword),
		rule(`\b(bool|char|f32|f64|i8|i16|i32|i64|i128|isize|str|u8|u16|u32|u64|u128|usize|String|Vec|Box|Option|Result|Some|None|Ok|Err)\b`, styleType),
		rule(`\b(println!|print!|eprintln!|eprint!|format!|vec!|panic!|assert!|assert_eq!|assert_ne!|todo!|unimplemented!|unreachable!|dbg!)\b`, styleBuiltin),
		rule(`\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?(?:_[a-z0-9]+)?\b|0x[0-9a-fA-F_]+\b`, styleNumber),
		rule(`[:=!<>+\-*/%&|^~?]+`, styleOperator),
		rule(`[(){}\[\],;.]`, stylePunct),
	},
	"c": {
		rule(`//.*$`, styleComment),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`'(?:[^'\\]|\\.)'`, styleString),
		rule(`\b(auto|break|case|char|const|continue|default|do|double|else|enum|extern|float|for|goto|if|inline|int|long|register|restrict|return|short|signed|sizeof|static|struct|switch|typedef|union|unsigned|void|volatile|while)\b`, styleKeyword),
		rule(`\b(NULL|true|false|TRUE|FALSE|EOF|stdin|stdout|stderr)\b`, styleBool),
		rule(`\b(printf|scanf|fprintf|fscanf|sprintf|sscanf|malloc|calloc|realloc|free|memcpy|memmove|memset|memcmp|strlen|strcpy|strncpy|strcat|strcmp|strncmp|fopen|fclose|fread|fwrite|fgets|fputs|exit|abort)\b`, styleBuiltin),
		rule(`#\s*(?:include|define|undef|ifdef|ifndef|endif|if|elif|else|pragma|error|warning)\b`, styleOperator),
		rule(`\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?[fFlLuU]*\b|0x[0-9a-fA-F]+\b`, styleNumber),
		rule(`[:=!<>+\-*/%&|^~?]+`, styleOperator),
		rule(`[(){}\[\],;.]`, stylePunct),
	},
	"ruby": {
		rule(`#.*$`, styleComment),
		rule(`"(?:[^"\\#]|\\.|\#\{[^}]*\})*"`, styleString),
		rule(`'(?:[^'\\]|\\.)*'`, styleString),
		rule(`\b(BEGIN|END|alias|and|begin|break|case|class|def|defined\?|do|else|elsif|end|ensure|false|for|if|in|module|next|nil|not|or|redo|rescue|retry|return|self|super|then|true|undef|unless|until|when|while|yield)\b`, styleKeyword),
		rule(`\b(puts|print|p|pp|gets|require|require_relative|include|extend|attr_reader|attr_writer|attr_accessor|raise|fail|catch|throw|lambda|proc|Array|Hash|String|Integer|Float|Symbol|Range|Regexp|File|IO|Dir)\b`, styleBuiltin),
		rule(`:[a-zA-Z_]\w*`, styleType),
		rule(`\b\d+(?:\.\d+)?\b|0x[0-9a-fA-F]+\b`, styleNumber),
		rule(`[:=!<>+\-*/%&|^~?]+`, styleOperator),
	},
}

// alias ts rules to js rules with extras
func init() {
	tsRules := make([]tokenRule, 0, len(langRules["js"])+3)
	tsRules = append(tsRules,
		rule(`//.*$`, styleComment),
		rule(`"(?:[^"\\]|\\.)*"`, styleString),
		rule(`'(?:[^'\\]|\\.)*'`, styleString),
		rule("`(?:[^`\\\\]|\\\\.)*`", styleString),
		rule(`\b(break|case|catch|class|const|continue|debugger|default|delete|do|else|export|extends|finally|for|from|function|if|import|in|instanceof|let|new|of|return|static|super|switch|this|throw|try|typeof|var|void|while|with|yield|async|await|interface|type|enum|implements|namespace|declare|abstract|readonly|as|satisfies|override|keyof|infer)\b`, styleKeyword),
		rule(`\b(true|false|null|undefined|never|unknown|any|void)\b`, styleBool),
		rule(`\b(string|number|boolean|object|symbol|bigint|Array|Record|Partial|Required|Pick|Omit|Readonly|Promise|Map|Set)\b`, styleType),
		rule(`\b(console|Math|JSON|Object|Array|String|Number|Boolean|Symbol|parseInt|parseFloat|isNaN|isFinite|setTimeout|setInterval|fetch|require|module|exports)\b`, styleBuiltin),
		rule(`\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b|0x[0-9a-fA-F]+\b`, styleNumber),
		rule(`[:=!<>+\-*/%&|^~?]+`, styleOperator),
		rule(`[(){}\[\],;.]`, stylePunct),
	)
	langRules["ts"] = tsRules
}

// ── colorLine ─────────────────────────────────────────────────────────────────

// colorLine returns a per-rune style slice for the given line.
// len(result) == len([]rune(line))
func colorLine(line, lang string) []tcell.Style {
	runes := []rune(line)
	styles := make([]tcell.Style, len(runes))
	for i := range styles {
		styles[i] = styleDefault
	}
	rules, ok := langRules[lang]
	if !ok || lang == "" {
		return styles
	}

	used := make([]bool, len(runes))
	for _, r := range rules {
		matches := r.re.FindAllStringIndex(line, -1)
		for _, m := range matches {
			rs := utf8.RuneCountInString(line[:m[0]])
			re := utf8.RuneCountInString(line[:m[1]])
			for i := rs; i < re && i < len(runes); i++ {
				if !used[i] {
					styles[i] = r.style
					used[i] = true
				}
			}
		}
	}
	return styles
}

package fs

import (
	"path/filepath"
	"strings"
)

func DetectLang(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.ToLower(filepath.Base(name))

	if lang, ok := extensions[ext]; ok {
		return lang
	}

	if lang, ok := fileNames[base]; ok {
		return lang
	}

	return ""
}

var fileNames = map[string]string{
	"makefile":         "makefile",
	"dockerfile":       "dockerfile",
	"containerfile":    "dockerfile",
	"jenkinsfile":      "groovy",
	"vagrantfile":      "ruby",
	".eslintrc":        "json",
	".babelrc":         "json",
	"package.json":     "json",
	"tsconfig.json":    "json",
	".env":             "shell",
	".gitignore":       "gitignore",
	".gitattributes":   "gitattributes",
	"cmakelists.txt":   "cmake",
	"go.mod":           "go",
	"go.sum":           "go",
	"cargo.toml":       "toml",
	"cargo.lock":       "toml",
	"gemfile":          "ruby",
	"rakefile":         "ruby",
	"podfile":          "ruby",
	"brewfile":         "ruby",
	"procfile":         "shell",
	"requirements.txt": "text",
}

var extensions = map[string]string{
	".go":    "go",
	".rs":    "rust",
	".c":     "c",
	".h":     "c",
	".cc":    "cpp",
	".cpp":   "cpp",
	".cxx":   "cpp",
	".hh":    "cpp",
	".hpp":   "cpp",
	".cs":    "csharp",
	".java":  "java",
	".kt":    "kotlin",
	".kts":   "kotlin",
	".swift": "swift",
	".m":     "objc",
	".mm":    "objc",
	".v":     "v",
	".zig":   "zig",
	".nim":   "nim",
	".d":     "d",
	".ex":    "elixir",
	".exs":   "elixir",
	".erl":   "erlang",
	".hrl":   "erlang",
	".ml":    "ocaml",
	".mli":   "ocaml",
	".hs":    "haskell",
	".lhs":   "haskell",
	".fs":    "fsharp",
	".fsi":   "fsharp",
	".fsx":   "fsharp",
	".clj":   "clojure",
	".cljs":  "clojure",
	".scala": "scala",
	".sc":    "scala",

	// Web / scripting
	".ts":     "typescript",
	".tsx":    "tsx",
	".js":     "javascript",
	".jsx":    "jsx",
	".mjs":    "javascript",
	".cjs":    "javascript",
	".py":     "python",
	".pyw":    "python",
	".rb":     "ruby",
	".php":    "php",
	".lua":    "lua",
	".r":      "r",
	".perl":   "perl",
	".pl":     "perl",
	".pm":     "perl",
	".groovy": "groovy",
	".gvy":    "groovy",
	".dart":   "dart",

	// Shell
	".sh":   "bash",
	".bash": "bash",
	".zsh":  "bash",
	".fish": "fish",
	".ps1":  "powershell",
	".psm1": "powershell",
	".bat":  "batch",
	".cmd":  "batch",

	// Markup / data
	".html":  "html",
	".htm":   "html",
	".xml":   "xml",
	".svg":   "xml",
	".css":   "css",
	".scss":  "scss",
	".sass":  "sass",
	".less":  "less",
	".json":  "json",
	".jsonc": "json",
	".yaml":  "yaml",
	".yml":   "yaml",
	".toml":  "toml",
	".ini":   "ini",
	".cfg":   "ini",
	".conf":  "ini",
	".env":   "shell",
	".md":    "markdown",
	".mdx":   "markdown",
	".rst":   "rst",
	".txt":   "text",
	".csv":   "csv",
	".tsv":   "csv",

	// Query
	".sql":     "sql",
	".graphql": "graphql",
	".gql":     "graphql",
	".prisma":  "prisma",

	// Templates
	".tmpl":     "html",
	".tpl":      "html",
	".jinja":    "jinja",
	".j2":       "jinja",
	".hbs":      "handlebars",
	".mustache": "mustache",
	".njk":      "nunjucks",
	".ejs":      "javascript",

	// Config / build
	".gradle": "groovy",
	".cmake":  "cmake",
	".tf":     "hcl",
	".tfvars": "hcl",
	".hcl":    "hcl",
	".nix":    "nix",
	".dhall":  "dhall",
	".proto":  "protobuf",
	".thrift": "thrift",
	".avsc":   "json",

	// Misc
	".ipynb": "jupyter",
	".tex":   "latex",
	".bib":   "bibtex",
	".vim":   "vim",
	".el":    "elisp",
	".lisp":  "lisp",
	".wasm":  "wasm",
}

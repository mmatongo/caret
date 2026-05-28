package tools

func Default() (*Registry, *ShellTool) {
	r := NewRegistry()
	shell := NewShellTool()

	r.Register(ReadFile())
	r.Register(ListDir())
	r.Register(Glob())
	r.Register(Search())
	r.Register(WriteFile())
	r.Register(MultiEdit())
	r.Register(shell)

	return r, shell
}

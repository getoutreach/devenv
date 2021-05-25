package completion

import (
	"fmt"
	"os"
	"text/template"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/urfave/cli/v2"
)

const (
	ShellBash = "bash"
	ShellZSH  = "zsh"
)

//nolint:gochecknoglobals
var (
	completionZSHTemplate = `
#compdef {{ .appName }}
_cli_zsh_autocomplete() {

	local -a opts
	local cur
	cur=${words[-1]}
	if [[ "$cur" == "-"* ]]; then
		opts=("${(@f)$(_CLI_ZSH_AUTOCOMPLETE_HACK=1 ${words[@]:0:#words[@]-1} ${cur} --generate-bash-completion)}")
	else
		opts=("${(@f)$(_CLI_ZSH_AUTOCOMPLETE_HACK=1 ${words[@]:0:#words[@]-1} --generate-bash-completion)}")
	fi

	if [[ "${opts[1]}" != "" ]]; then
		_describe 'values' opts
	else
		_files
	fi

	return
}
compdef _cli_zsh_autocomplete {{ .appName }}
	`
	completionBashTemplate = `
_cli_bash_autocomplete() {
	if [[ "${COMP_WORDS[0]}" != "source" ]]; then
		local cur opts base
		COMPREPLY=()
		cur="${COMP_WORDS[COMP_CWORD]}"
		if [[ "$cur" == "-"* ]]; then
			opts=$( ${COMP_WORDS[@]:0:$COMP_CWORD} ${cur} --generate-bash-completion )
		else
			opts=$( ${COMP_WORDS[@]:0:$COMP_CWORD} --generate-bash-completion )
		fi
		COMPREPLY=( $(compgen -W "${opts}" -- ${cur}) )
		return 0
	fi
}

complete -o bashdefault -o default -o nospace -F _cli_bash_autocomplete {{ .appName }}
unset PROG
	`

	completionLongDesc = `
		completion generates completion for a given shell
	`
	completionExample = `
	# BASH
	## Installing bash completion
	## please ensure that bash completion is installed on your platform
	## macOS:
	brew install bash-completion (Bash 4.2+: bash-completion@2)
	## linux:
	sudo apt install bash-completion

	## Add to your ~/.bashrc
	source <(devenv --skip-update completion bash)
	## Restart your shell.


	# ZSH
	## Add to your ~/.zshrc or ~/.zsh_profile
	source <(devenv --skip-update completion zsh)
	## Restart your shell.
	`
)

type Options struct {
	Shell string
}

func NewOptions() *Options {
	return &Options{}
}

func NewCmdCompletion() *cli.Command {
	o := NewOptions()

	return &cli.Command{
		Name:        "completion",
		Usage:       "Generate shell completion",
		Description: cmdutil.NewDescription(completionLongDesc, completionExample),
		Flags:       []cli.Flag{},
		Action: func(c *cli.Context) error {
			shell := c.Args().First()
			if shell == "" {
				return fmt.Errorf("missing shell")
			}

			switch shell {
			case ShellZSH:
			case ShellBash:
			default:
				return fmt.Errorf("unsupported shell '%v'", shell)
			}

			o.Shell = shell

			return o.Run()
		},
	}
}

func (o *Options) Run() error {
	templateSource := ""
	if o.Shell == ShellZSH {
		templateSource = completionZSHTemplate
	} else if o.Shell == ShellBash {
		templateSource = completionBashTemplate
	}

	return template.Must(template.New("completion").Parse(templateSource)).Execute(os.Stdout, map[string]string{
		"appName": "devenv",
	})
}

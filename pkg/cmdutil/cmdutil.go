package cmdutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/getoutreach/devenv/pkg/kube"
	"github.com/getoutreach/gobox/pkg/app"
	"github.com/getoutreach/gobox/pkg/trace"
	"github.com/manifoldco/promptui"
	"github.com/mitchellh/go-wordwrap"
	"github.com/pkg/errors"

	olog "github.com/getoutreach/gobox/pkg/log"
)

const (
	Indentation = "   "
	LineLen     = 80
	helpStr     = "Found a bug? Need help? https://outreach-io.atlassian.net/wiki/spaces/EN/pages/1072791750"
)

func CLIStringSliceToStringSlice(origSlice []string, newSlice *[]string) {
	if len(origSlice) != len(*newSlice) {
		*newSlice = make([]string, len(origSlice))
	}
	copy(*newSlice, origSlice)
}

// NewDescription creates a description from a long desc
// and examples. This also formats them and normalizes the formatting.
func NewDescription(desc, examples string) string {
	normalizedDesc := Normalize(desc)
	normalizedExamples := Normalize(examples)

	return normalizedDesc + "\n\nEXAMPLES:\n" + normalizedExamples + "\n\n" + helpStr
}

// Normalize takes a string and normalizes it.
func Normalize(s string) string {
	indentedLines := []string{}
	for _, line := range strings.Split(wordwrap.WrapString(s, LineLen), "\n") {
		trimmed := strings.TrimSpace(line)
		indented := "   " + trimmed
		indentedLines = append(indentedLines, indented)
	}

	if strings.TrimSpace(indentedLines[len(indentedLines)-1]) == "" {
		// found extra newline, remove it
		indentedLines = indentedLines[:len(indentedLines)-1]
	}

	return strings.Join(indentedLines, "\n")
}

func GetYesOrNoInput(ctx context.Context) (bool, error) {
	prompt := promptui.Select{
		Label: "Select",
		Items: []string{"Yes", "No"},
	}

	_, resp, err := prompt.Run()
	if err != nil {
		return false, err
	}

	if strings.EqualFold(resp, "yes") {
		return true, nil
	}

	return false, nil
}

// RunKubernetesCommand runs a command with KUBECONFIG set. This command runs in the
// provided working directory
func RunKubernetesCommand(ctx context.Context, wd string, onlyOutputOnError bool, name string, args ...string) error {
	ctx = trace.StartCall(ctx, "devenvutil.RunKubernetesCommand", olog.F{"command": name})
	defer trace.EndCall(ctx)

	cmd, err := CreateKubernetesCommand(ctx, wd, name, args...)
	if err != nil {
		return err
	}

	if !onlyOutputOnError {
		cmd.Stdout = os.Stdout
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	b, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(string(b))
	}
	return err
}

// CreateKubernetesCommand is like RunKubernetesCommand but returns the command
func CreateKubernetesCommand(ctx context.Context, wd, command string, args ...string) (*exec.Cmd, error) {
	ctx = trace.StartCall(ctx, "devenvutil.CreateKubernetesCommand", olog.F{"command": command})
	defer trace.EndCall(ctx)

	kubeConfPath, err := kube.GetKubeConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get kubeconfig")
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KUBECONFIG=%s", kubeConfPath),
		fmt.Sprintf("DEVENV_VERSION=%s", app.Version),
	)
	return cmd, nil
}

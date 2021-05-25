package containerruntime

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/getoutreach/devenv/pkg/cmdutil"
	"github.com/getoutreach/gobox/pkg/trace"

	olog "github.com/getoutreach/gobox/pkg/log"
)

// RemoveImage deletes an image from the containerruntime
func RemoveImage(ctx context.Context, image string) error {
	ctx = trace.StartCall(ctx, "containerruntime.RemoveImage", olog.F{"image": image})
	defer trace.EndCall(ctx)

	if !HasImage(ctx, image) {
		return nil
	}

	err := cmdutil.RunKubernetesCommand(
		ctx,
		"",
		false,
		"docker",
		"exec",
		ContainerName,
		"ctr",
		"--namespace",
		"k8s.io",
		"images",
		"rm",
		image,
	)
	return trace.SetCallStatus(ctx, err)
}

// HasImage checks to see if the containerruntime has the given image in its cache
func HasImage(ctx context.Context, image string) bool {
	ctx = trace.StartCall(ctx, "containerruntime.HasImage", olog.F{"image": image})
	defer trace.EndCall(ctx)

	//nolint:gosec // Why: We need to pass args.
	cmd := exec.CommandContext(ctx, "docker",
		"exec",
		ContainerName,
		"ctr", "--namespace", "k8s.io", "images", "list", "-q",
		fmt.Sprintf("name==%s", image),
	)
	b, err := cmd.Output()
	if err != nil {
		return false
	}

	// If we have the ref, it'll echo it back to us.
	if strings.TrimSpace(string(b)) == image {
		return true
	}

	return false
}

// PullImage fetches an image inside for our containerruntime to use.
func PullImage(ctx context.Context, image string) error {
	ctx = trace.StartCall(ctx, "containerruntime.PullImage", olog.F{"image": image})
	defer trace.EndCall(ctx)

	homedir, err := os.UserHomeDir()
	if err != nil {
		return trace.SetCallStatus(ctx, err)
	}

	b, err := ioutil.ReadFile(filepath.Join(homedir, ".outreach", "imgpullsecret.json"))
	if err != nil {
		return trace.SetCallStatus(ctx, err)
	}

	userpass := fmt.Sprintf("_json_key:%s", string(b))

	err = cmdutil.RunKubernetesCommand(
		ctx,
		"",
		false,
		"docker",
		"exec",
		ContainerName,
		"ctr",
		"--namespace",
		"k8s.io",
		"images",
		"rm",
		"--user",
		userpass,
		image,
	)
	return trace.SetCallStatus(ctx, err)
}

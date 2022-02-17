package provision

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func startDockerForMac(ctx context.Context, d dockerclient.APIClient, log logrus.FieldLogger) error {
	// Give Docker for Mac time to stop.
	time.Sleep(2 * time.Second)

	cmd := exec.CommandContext(ctx, "open", "-a", "Docker")
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(errors.Wrap(err, string(out)), "failed to open Docker for Mac (try starting it manually)")
	}

	ticker := time.NewTicker(7 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_, err := d.ServerVersion(ctx)
			if err != nil {
				log.WithError(err).Info("Waiting for Docker for Mac to start ...")
				continue
			}

			return nil
		}
	}
}

func reconcileDockerForMacConfig(_ context.Context, settingsFile string) (bool, error) { //nolint:funlen
	b, err := os.ReadFile(settingsFile)
	if err != nil {
		return false, err
	}

	var settings map[string]interface{}
	if err2 := json.Unmarshal(b, &settings); err2 != nil {
		return false, err
	}

	recommendedCPU := 4
	recommendedMemory := 8192
	// 208 GB
	recommendedStorage := 212992
	requiredMounts := map[string]bool{
		"/Users":               true,
		"/private/var/folders": true,
	}

	modified := false
	if cpu, ok := settings["cpus"].(float64); ok {
		if int(cpu) != recommendedCPU {
			modified = true
			settings["cpus"] = recommendedCPU
		}
	}

	if memory, ok := settings["memoryMiB"].(float64); ok {
		if int(memory) != recommendedMemory {
			modified = true
			settings["memoryMiB"] = recommendedMemory
		}
	}

	if diskSpace, ok := settings["diskSizeMiB"].(float64); ok {
		// We only set disk space if it's below our recommended storage
		// level
		if int(diskSpace) < recommendedStorage {
			modified = true
			settings["diskSizeMiB"] = recommendedStorage
		}
	}

	if mounts, ok := settings["filesharingDirectories"].([]interface{}); ok {
		for _, m := range mounts {
			mount, ok := m.(string)
			if !ok {
				continue
			}

			if _, ok = requiredMounts[mount]; !ok {
				modified = true
				newMounts := make([]interface{}, 0)
				for mp := range requiredMounts {
					newMounts = append(newMounts, mp)
				}
				settings["filesharingDirectories"] = newMounts

				// we found one path that wasn't in our requiredMounts
				// so we just overwrite the entire thing and stop processing
				break
			}
		}
	}

	if modified {
		b, err = json.MarshalIndent(&settings, "", "  ")
		if err != nil {
			return false, err
		}
	}

	//nolint:gosec // This is what is default for the config
	return modified, os.WriteFile(settingsFile, b, 0o644)
}

func (o *Options) configureDockerForMac(ctx context.Context) error {
	settingsFile := filepath.Join(o.homeDir, "Library", "Group Containers", "group.com.docker", "settings.json")
	if _, err := os.Stat(settingsFile); os.IsNotExist(err) {
		// start docker for mac
		o.log.Info("Initializing Docker for Mac")
		err = startDockerForMac(ctx, o.d, o.log)
		if err != nil {
			return errors.Wrap(err, "failed to start Docker for Mac")
		}
	}

	modified, err := reconcileDockerForMacConfig(ctx, settingsFile)
	if err != nil {
		o.log.WithError(err).Warn("failed to reconcile Docker for Mac settings")
		return nil
	}

	// if not modified, we don't care :rocket:
	if !modified {
		return nil
	}

	o.log.Info("Updated Docker for Mac configuration")

	// Restart Docker for Mac so it loads the settings
	o.log.Info("Restarting Docker for Mac")
	cmd := exec.CommandContext(ctx, "osascript", "-e", "quit app \"Docker\"")
	cmd.Env = os.Environ()
	err = cmd.Run()
	if err != nil {
		o.log.WithError(err).Warn("failed to stop Docker for Mac")
	}
	return startDockerForMac(ctx, o.d, o.log)
}

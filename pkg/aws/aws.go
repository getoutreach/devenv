package aws

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/getoutreach/devenv/internal/alert"
	"github.com/getoutreach/gobox/pkg/async"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/versent/saml2aws/v2/pkg/awsconfig"
)

// CredentialOptions configures what credentials are provided
type CredentialOptions struct {
	// Log is a logger to be used for informational logging.
	// if not supplied no output aside from prompting will be displayed
	Log logrus.FieldLogger

	// Role to assume for the user
	Role string

	// Profile to use
	Profile string
}

func DefaultCredentialOptions() *CredentialOptions {
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = "default"
	}

	return &CredentialOptions{
		Role:    "arn:aws:iam::182192988802:role/okta_eng_readonly_role",
		Profile: profile,
	}
}

// assumedToRole takes an assumed-role arn and converts it to the
// arn of the role that was assumed
func assumedToRole(assumedRole string) string {
	spl := strings.Split(assumedRole, "/")
	if len(spl) != 3 {
		return assumedRole
	}

	spl[0] = strings.Replace(strings.Replace(spl[0], "assumed-role", "role", 1), "sts", "iam", 1)
	return strings.Join(spl[:2], "/")
}

// EnsureValidCredentials ensures that the current AWS credentials are valid
// and if they can expire it is attempted to rotate them when they are expired
// via saml2aws
func EnsureValidCredentials(ctx context.Context, copts *CredentialOptions) error { //nolint:funlen
	if _, ok := os.LookupEnv("CI"); ok {
		return nil
	}

	if copts == nil {
		copts = DefaultCredentialOptions()
	}

	needsNewCreds := false
	reason := ""

	if creds, err := awsconfig.NewSharedCredentials(copts.Profile, "").Load(); err == nil {
		// Check, via the principal_arn, if the creds match the role we want
		if creds.PrincipalARN != "" && assumedToRole(creds.PrincipalARN) != copts.Role {
			if copts.Log != nil {
				reason = "Refreshing AWS credentials due to existing credentials using a different role"
			}
			needsNewCreds = true
		}

		// Attempt to refresh the aws credentials via saml2aws if
		// they can expire. If they can refresh within 3 minutes of
		// the expiration period or if they are expired.
		if !creds.Expires.IsZero() && time.Now().Add(3*time.Minute).After(creds.Expires) {
			reason = "Credentials are expired"
			needsNewCreds = true
		}
	} else if err != nil {
		// if we failed to load the credentials, assume they need to be refreshed
		reason = "No existing credentials"
		needsNewCreds = true
	}

	// Reissue the AWS credentials
	if needsNewCreds {
		if _, err := exec.LookPath("saml2aws"); err != nil {
			return fmt.Errorf("failed to find saml2aws, please run orc setup")
		}

		if copts.Log != nil {
			copts.Log.WithField("reason", reason).Info("Obtaining AWS credentials via Okta")
		}

		var loggedInToAWS bool
		go func(ctx context.Context) {
			// Sleep for 15 seconds the first time before checking to alert for AWS login.
			async.Sleep(ctx, time.Second*15)
			for {
				if loggedInToAWS {
					// Is logged in, don't alert.
					return
				}

				alert.Alert("Your tunnel needs elevated permissions!")

				// Sleep for 25 seconds between each alert after the first one.
				async.Sleep(ctx, time.Second*25)
			}
		}(ctx)

		//nolint:gosec // Why: What other option do I have
		cmd := exec.CommandContext(ctx, "saml2aws", "login", "--profile", copts.Profile, "--role", copts.Role, "--force")
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return errors.Wrap(err, "failed to refresh AWS credentials via saml2aws")
		}
		loggedInToAWS = true
	}

	return nil
}
